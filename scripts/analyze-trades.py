#!/usr/bin/env python3
"""Разбор закрытых сделок: качество входа, геометрия стопа, поведение выхода.

Считает то, чего нет в БД: где реально стоял рынок в момент входа, сколько
сделок умерло в свече входа, и что цена делала ПОСЛЕ выхода. Пишет две таблицы:

  data/analysis/trades_enriched.csv — одна строка на сделку + производные метрики
  data/analysis/metrics.csv         — append-only срез по экспериментам на каждый прогон

metrics.csv — журнал прогресса: одна строка = (снимок, эксперимент). Сравнивая
снимки, видно, двигают ли правки метрики в нужную сторону.

Заархивированные в админке периоды по умолчанию исключаются — разбор идёт по той
же выборке, что видна на экране. Вернуть их: --include-archived.

Использование:
    python3 scripts/analyze-trades.py [--db data/trades.db] [--history data/history]
                                      [--label "после фикса фила"] [--no-write]
                                      [--archives data/archives.json] [--include-archived]
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import sqlite3
import subprocess
from datetime import datetime, timezone

import numpy as np
import pandas as pd

BAR = pd.Timedelta(minutes=5)
SESSION_END = pd.Timestamp("1900-01-01 15:40", tz="UTC").time()  # 18:40 MSK
MSK_OFFSET = pd.Timedelta(hours=3)


# --- загрузка ---------------------------------------------------------------

def load_trades(db: str) -> pd.DataFrame:
    with sqlite3.connect(db) as con:
        t = pd.read_sql("SELECT * FROM closed_trades", con)
    return _prepare(t)


def load_trades_json(path: str) -> pd.DataFrame:
    """Сделки из incident.json (кнопка «Скачать incident.json» на /export).

    Имена полей в выгрузке один-в-один с колонками closed_trades — это держит
    тест TestВыгрузкаЗеркалитСхемуТаблицы, поэтому дальше данные обрабатываются
    тем же кодом, что и прочитанные из БД, без таблицы соответствий.
    """
    with open(path, encoding="utf-8") as fh:
        bundle = json.load(fh)
    if bundle.get("truncated"):
        print("ВНИМАНИЕ: выгрузка обрезана по лимиту — период неполон, сузьте даты.\n")
    return _prepare(pd.DataFrame(bundle.get("trades") or []))


def load_rejected_json(path: str) -> pd.DataFrame:
    """Отклонённые сигналы из той же выгрузки. Из БД их читать нечем: --db
    работает с closed_trades, отдельного пути к rejected_signals здесь нет."""
    with open(path, encoding="utf-8") as fh:
        bundle = json.load(fh)
    return pd.DataFrame(bundle.get("rejected_signals") or [])


def _prepare(t: pd.DataFrame) -> pd.DataFrame:
    """Общая подготовка: одна для БД и для JSON, иначе метрики разъедутся."""
    if t.empty:
        return t
    t = t.sort_values("opened_at").reset_index(drop=True)
    t["entry_bar_ts"] = pd.to_datetime(t["entry_bar_time"], utc=True, format="ISO8601")
    t["open_utc"] = pd.to_datetime(t["opened_at"]).sub(MSK_OFFSET).dt.tz_localize("UTC")
    t["close_utc"] = pd.to_datetime(t["closed_at"]).sub(MSK_OFFSET).dt.tz_localize("UTC")
    t["sgn"] = np.where(t.direction == "BUY", 1, -1)
    t["level"] = np.where(t.direction == "BUY", t.breakout_upper, t.breakout_lower)
    return t


def load_archives(path: str) -> list[dict]:
    """Архивы админки (data/archives.json). Нет файла — нечего исключать."""
    if not path or not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        return json.load(f) or []


def drop_archived(t: pd.DataFrame, archives: list[dict]):
    """Выкидывает сделки заархивированных периодов — ровно как их прячет админка.

    Возвращает (оставшиеся сделки, [(date_from, date_to, сколько выкинуто)]).
    """
    dropped = []
    keep = pd.Series(True, index=t.index)
    for a in archives:
        lo, hi = a.get("date_from", ""), a.get("date_to", "")
        if not lo or not hi:
            continue
        inside = t.trading_date.between(lo, hi)
        dropped.append((lo, hi, int((inside & keep).sum())))
        keep &= ~inside
    return t[keep].reset_index(drop=True), dropped


def load_history(path: str) -> dict[str, pd.DataFrame]:
    out = {}
    for f in sorted(os.listdir(path)):
        if not f.endswith(".csv"):
            continue
        d = pd.read_csv(os.path.join(path, f), parse_dates=["timestamp"])
        out[f[:-4]] = d.set_index("timestamp").sort_index()
    return out


# --- производные метрики ----------------------------------------------------

def enrich(t: pd.DataFrame, H: dict[str, pd.DataFrame]) -> pd.DataFrame:
    """Добавляет диагностику, которую нельзя посчитать без исторических баров."""
    rows = []
    for _, r in t.iterrows():
        d = H.get(r.ticker)
        bar = nxt = None
        if d is not None:
            b = d.loc[d.index == r.entry_bar_ts]
            n = d.loc[d.index == r.entry_bar_ts + BAR]
            bar = b.iloc[0] if len(b) else None
            nxt = n.iloc[0] if len(n) else None

        # Состояние позиции на открытии следующей свечи: <0 — уже против нас.
        pos_at_open = (r.sgn * (nxt.open - r.entry_price) / r.r_distance
                       if nxt is not None and r.r_distance > 0 else np.nan)
        # Где закрылась свеча, в которой записан фил.
        fillbar_close = (r.sgn * (bar.close - r.entry_price) / r.r_distance
                         if bar is not None and r.r_distance > 0 else np.nan)
        # Был ли уровень пробоя реально пройден рынком в свече входа.
        level_touched = (bool(bar.low <= r.level <= bar.high)
                         if bar is not None and r.level > 0 else None)

        # Что цена дала ПОСЛЕ выхода — до конца сессии того же дня.
        mfe_after = np.nan
        if d is not None:
            w = d.loc[(d.index > r.close_utc) & (d.index.date == r.entry_bar_ts.date())]
            w = w[w.index.time <= SESSION_END]
            if len(w) and r.r_distance > 0:
                fav = w.high.max() if r.sgn > 0 else w.low.min()
                mfe_after = (fav - r.entry_price) * r.sgn / r.r_distance

        rows.append((pos_at_open, fillbar_close, level_touched, mfe_after))

    t = t.copy()
    t["pos_at_open_r"] = [x[0] for x in rows]
    t["fillbar_close_r"] = [x[1] for x in rows]
    t["level_touched"] = [x[2] for x in rows]
    t["mfe_after_exit_r"] = [x[3] for x in rows]

    t["r_bps"] = 1e4 * t.r_distance / t.entry_price
    t["same_bar"] = (t.open_utc.dt.floor("5min") == t.close_utc.dt.floor("5min")).astype(int)
    # Мёртвый вход: позиция уже за стопом, едва открывшись.
    t["dead_on_arrival"] = (t.pos_at_open_r < -1).astype("Int64")
    # Недобор по выходу: сколько R осталось на столе после фиксации.
    t["left_on_table_r"] = t.mfe_after_exit_r - t.pnl_r
    return t


def per_experiment(t: pd.DataFrame) -> pd.DataFrame:
    g = t.groupby("experiment_id")
    m = pd.DataFrame({
        "trades": g.size(),
        "sum_r": g.pnl_r.sum(),
        "expectancy_r": g.pnl_r.mean(),
        "win_rate": g.is_winner.mean(),
        "avg_win_r": g.apply(lambda x: x.loc[x.is_winner == 1, "pnl_r"].mean(), include_groups=False),
        "avg_loss_r": g.apply(lambda x: x.loc[x.is_winner == 0, "pnl_r"].mean(), include_groups=False),
        "median_r_bps": g.r_bps.median(),
        "same_bar_share": g.same_bar.mean(),
        "dead_on_arrival_share": g.dead_on_arrival.mean(),
        "median_pos_at_open_r": g.pos_at_open_r.median(),
        "median_left_on_table_r": g.left_on_table_r.median(),
    })
    return m.reset_index()


# --- вывод ------------------------------------------------------------------

def git_rev() -> str:
    try:
        return subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"], text=True, stderr=subprocess.DEVNULL
        ).strip()
    except Exception:
        return "unknown"


def report(t: pd.DataFrame, m: pd.DataFrame) -> None:
    n = len(t)
    print(f"Сделок: {n}   период: {t.trading_date.min()} → {t.trading_date.max()}")
    print(f"Итог: {t.pnl_r.sum():+.1f}R   expectancy: {t.pnl_r.mean():+.3f}R   "
          f"win rate: {t.is_winner.mean():.1%}")

    print("\n— Качество входа —")
    dead = t.dead_on_arrival.sum()
    print(f"  уже за стопом на открытии следующей свечи: {dead}/{t.pos_at_open_r.notna().sum()}"
          f" ({dead / max(t.pos_at_open_r.notna().sum(), 1):.0%})")
    print(f"  вход и выход в одной 5-мин свече:          {t.same_bar.sum()}/{n}"
          f" ({t.same_bar.mean():.0%})")
    print(f"  медиана позиции сразу после входа:         {t.pos_at_open_r.median():+.2f}R")
    if t.level_touched.notna().any():
        never = (t.level_touched == False).sum()  # noqa: E712
        print(f"  уровень пробоя не пройден в свече входа:   {never}/{t.level_touched.notna().sum()}")

    print("\n— Геометрия стопа —")
    print(f"  медиана R: {t.r_bps.median():.1f} б.п.   нижний квартиль: {t.r_bps.quantile(.25):.1f} б.п.")

    # Сайзинг: с миграции 007 в БД есть объём ДО капа по кэшу. Без него факт капа
    # выводился только косвенно — через разброс gross_pnl/pnl_r между сделками,
    # и стоил целого круга разбирательств (docs/analysis/0002-...).
    # ВНИМАНИЕ: в metrics.csv эти поля не добавлять — файл дописывается без
    # заголовка, новая колонка сдвинет поля в уже записанных строках.
    if "requested_quantity" in t.columns:
        sized = t[t.requested_quantity > 0]
        if len(sized):
            capped = sized[sized.quantity < sized.requested_quantity]
            print("\n— Сайзинг —")
            print(f"  кап по кэшу срезал объём: {len(capped)}/{len(sized)}"
                  f" ({len(capped) / len(sized):.0%})")
            r_rub = (sized.gross_pnl / sized.pnl_r).abs()
            r_rub = r_rub[r_rub.notna() & (r_rub > 0)]
            if len(r_rub):
                spread = r_rub.max() / r_rub.min() if r_rub.min() > 0 else float("inf")
                print(f"  цена 1R, руб: медиана {r_rub.median():.0f},"
                      f" от {r_rub.min():.0f} до {r_rub.max():.0f} (разброс x{spread:.1f})")
                if spread > 1.5:
                    print("  ВНИМАНИЕ: цена R гуляет между сделками — risk_per_trade_percent"
                          " не управляет размером, см. docs/analysis/0002-*")
            if len(capped):
                share = (capped.quantity / capped.requested_quantity)
                print(f"  на закэпленных взято от запрошенного: медиана {share.median():.0%},"
                      f" минимум {share.min():.0%}")

    if "bar_age_seconds" in t.columns and (t.bar_age_seconds > 0).any():
        ba = t.bar_age_seconds[t.bar_age_seconds > 0]
        print(f"\n— Лаг свечного фида —\n  возраст свечи входа: медиана {ba.median():.0f}с,"
              f" максимум {ba.max():.0f}с")

    print("\n— Выход —")
    tp = t[t.close_reason == "TAKE_PROFIT"]
    if len(tp) and tp.left_on_table_r.notna().any():
        kept = tp.left_on_table_r.dropna()
        print(f"  выходов по тейку: {len(tp)}, взято в среднем {tp.pnl_r.mean():.2f}R")
        print(f"  осталось на столе до конца сессии: медиана {kept.median():+.2f}R, "
              f"дали ещё >1R: {(kept > 1).sum()}/{len(kept)}")
    else:
        print("  выходов по тейку нет")

    print("\n— По экспериментам —")
    cols = ["experiment_id", "trades", "sum_r", "expectancy_r", "win_rate",
            "median_r_bps", "same_bar_share", "dead_on_arrival_share"]
    print(m[cols].to_string(index=False, float_format=lambda v: f"{v:.3f}"))


# --- водяной знак разбора ------------------------------------------------

REVIEW_STATE = "data/analysis/review-state.json"


def config_fingerprint(cfg_path: str) -> tuple[str, str, dict]:
    """Отпечаток боевого конфига: блоки strategy и risk, без комментариев.

    Считается по разобранному YAML, а не по тексту файла: иначе правка
    комментария выглядела бы как смена механики. Ловит ровно то, что однажды
    стоило дня разбирательств — незамеченное расхождение конфига со сделками
    (docs/analysis/0002-live-config-drift-and-cash-sizing.md).
    """
    try:
        import yaml
    except ImportError:
        return "", "PyYAML не установлен — отпечаток конфига не считается", {}
    try:
        with open(cfg_path, encoding="utf-8") as fh:
            cfg = yaml.safe_load(fh)
    except OSError:
        return "", f"конфиг не найден: {cfg_path}", {}

    material = {
        "risk": cfg.get("risk", {}),
        "costs": cfg.get("costs", {}),
        "experiments": {e.get("id"): e.get("strategy", {})
                        for e in cfg.get("experiments", [])},
    }
    blob = json.dumps(material, sort_keys=True, ensure_ascii=False, default=str)
    digest = "sha256:" + hashlib.sha256(blob.encode("utf-8")).hexdigest()[:16]

    risk = material["risk"]
    gates = {e.get("min_stop_bps") for e in material["experiments"].values()}
    gate = gates.pop() if len(gates) == 1 else "разные"
    summary = (f"min_stop_bps={gate} risk={risk.get('risk_per_trade_percent')} "
               f"slippage={material['costs'].get('slippage_bps')}")
    # Отпечаток каждого слота отдельно: без него предупреждение сообщает «конфиг
    # изменился» и показывает одинаковые строки, потому что общая сводка не видит
    # параметров стратегий. Детектор, который не говорит ЧТО изменилось, читают
    # как ложную тревогу и через раз игнорируют.
    slots = {}
    for sid, params in material["experiments"].items():
        blob_s = json.dumps(params, sort_keys=True, ensure_ascii=False, default=str)
        slots[sid] = hashlib.sha256(blob_s.encode("utf-8")).hexdigest()[:12]
    return digest, summary, slots


def load_review_state(path: str) -> dict:
    try:
        with open(path, encoding="utf-8") as fh:
            return json.load(fh)
    except (OSError, json.JSONDecodeError):
        return {}


def report_review_delta(t_all: pd.DataFrame, state: dict, digest: str, summary: str,
                        slots: dict | None = None, partial: bool = False) -> None:
    """Печатает, что накопилось с прошлого разбора, и ругается на смену механики."""
    if not state:
        print("Прошлого разбора нет: водяной знак не выставлен "
              "(поставить: make analyze-mark LABEL=\"...\").\n")
        return

    last_id = int(state.get("last_trade_id", 0))
    fresh = int((t_all["id"] > last_id).sum()) if "id" in t_all.columns else 0
    print(f"Прошлый разбор: {state.get('last_review_at', '?')} "
          f"({state.get('label') or 'без пометки'}), последняя сделка id={last_id}.")
    print(f"С тех пор новых сделок: {fresh}." + (
        "  (по выгрузке — она уже сужена фильтром дат, это не вся база)" if partial else ""))

    was = state.get("config_fingerprint", "")
    if digest and was and digest != was:
        print()
        print("!!! КОНФИГ ИЗМЕНИЛСЯ С ПРОШЛОГО РАЗБОРА !!!")
        old_slots = state.get("config_slots") or {}
        new_slots = slots or {}
        changed = sorted(k for k in set(old_slots) | set(new_slots)
                         if old_slots.get(k) != new_slots.get(k))
        if changed:
            for k in changed:
                if k not in old_slots:
                    print(f"    + слот {k}: добавлен")
                elif k not in new_slots:
                    print(f"    − слот {k}: удалён")
                else:
                    print(f"    ~ слот {k}: параметры стратегии изменены")
        if state.get("config_summary") != summary:
            print(f"    механика: {state.get('config_summary', was)} → {summary}")
        elif not changed:
            print(f"    отпечаток {was} → {digest}, но сводка та же — "
                  f"изменилось что-то вне risk/costs/experiments")
        print("    Сделки до и после несравнимы: параметры чемпионов и docs/baseline.md")
        print("    недействительны, старый период нужно заархивировать "
              "(data/archives.json).")
    print()


def write_review_state(path: str, t_all: pd.DataFrame, label: str,
                       digest: str, summary: str, reviewed: int,
                       slots: dict | None = None) -> dict:
    last = t_all.iloc[t_all["id"].idxmax()] if "id" in t_all.columns and len(t_all) else None
    state = {
        "last_review_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "last_trade_id": int(last["id"]) if last is not None else 0,
        "last_trade_closed_at": str(last["closed_at"]) if last is not None else "",
        "trades_reviewed": int(reviewed),
        "label": label,
        "config_fingerprint": digest,
        "config_summary": summary,
        "config_slots": slots or {},
    }
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(state, fh, ensure_ascii=False, indent=2)
        fh.write("\n")
    return state


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default="data/trades.db")
    ap.add_argument("--from-json", default="",
                    help="incident.json с /export вместо копии БД "
                         "(тогда --db не используется)")
    ap.add_argument("--history", default="data/history")
    ap.add_argument("--out", default="data/analysis")
    ap.add_argument("--label", default="", help="пометка снимка в metrics.csv")
    ap.add_argument("--no-write", action="store_true", help="только отчёт, ничего не писать")
    ap.add_argument("--archives", default="data/archives.json",
                    help="JSON с архивами админки; их периоды исключаются из разбора")
    ap.add_argument("--include-archived", action="store_true",
                    help="считать по всем сделкам, включая заархивированные")
    ap.add_argument("--review-state", default=REVIEW_STATE,
                    help="файл с водяным знаком прошлого разбора")
    ap.add_argument("--config", default="configs/runs/portfolio-paper.yaml",
                    help="боевой конфиг: с него снимается отпечаток механики")
    ap.add_argument("--since-review", action="store_true",
                    help="считать только сделки, появившиеся после прошлого разбора")
    ap.add_argument("--mark-reviewed", action="store_true",
                    help="записать водяной знак: дата, последняя сделка, отпечаток конфига")
    args = ap.parse_args()

    src_json = bool(args.from_json)
    if src_json:
        t = load_trades_json(args.from_json)
        rejected = load_rejected_json(args.from_json)
        print(f"Источник: {args.from_json} "
              f"(сделок {len(t)}, отклонённых сигналов {len(rejected)})")
        if len(rejected):
            top = rejected.reason.value_counts().head(3)
            print("  причины отказов: "
                  + ", ".join(f"{r} — {n}" for r, n in top.items()))
        print()
        if t.empty:
            print("В выгрузке нет закрытых сделок.")
            return
    else:
        t = load_trades(args.db)
        if t.empty:
            print("В БД нет закрытых сделок.")
            return

    # Дельта считается до вырезания архивов: иначе «новых сделок» станет ноль
    # просто потому, что свежий период кто-то заархивировал. В JSON-режиме
    # выборка уже сужена фильтром выгрузки — об этом говорится явно.
    digest, summary, slots = config_fingerprint(args.config)
    state = load_review_state(args.review_state)
    report_review_delta(t, state, digest, summary, slots=slots, partial=src_json)

    if args.since_review:
        last_id = int(state.get("last_trade_id", 0))
        before = len(t)
        t = t[t["id"] > last_id]
        print(f"--since-review: осталось {len(t)} из {before} сделок (id > {last_id}).\n")
        if t.empty:
            print("С прошлого разбора новых сделок нет.")
            return

    if not args.include_archived:
        t, dropped = drop_archived(t, load_archives(args.archives))
        total_dropped = sum(n for _, _, n in dropped)
        if total_dropped:
            periods = ", ".join(f"{lo} — {hi} ({n})" for lo, hi, n in dropped if n)
            print(f"Исключено архивных сделок: {total_dropped} [{periods}]")
            print("Строки metrics.csv до и после архивации считаны по разным выборкам — "
                  "сравнивать их напрямую нельзя.\n")
        if t.empty:
            print("После исключения архивов закрытых сделок не осталось.")
            # Знак ставится и по пустой выборке: он про всю базу, а пустая
            # выборка после архивации — это ровно момент чистого старта.
            if args.mark_reviewed and not args.no_write:
                full = load_trades_json(args.from_json) if src_json else load_trades(args.db)
                st = write_review_state(args.review_state, full,
                                        args.label, digest, summary, reviewed=0, slots=slots)
                print(f"Водяной знак выставлен: {args.review_state} "
                      f"(последняя сделка id={st['last_trade_id']}, {st['config_summary']})")
            return

    t = enrich(t, load_history(args.history))
    m = per_experiment(t)
    report(t, m)

    # Водяной знак ставится ТОЛЬКО явным флагом. Если двигать его на каждом
    # прогоне, первый же случайный запуск съест границу и «новых сделок» больше
    # не будет — а именно ради них файл и заводился.
    if args.mark_reviewed:
        if args.no_write:
            print("\n--mark-reviewed несовместим с --no-write: знак не выставлен.")
        else:
            full = load_trades_json(args.from_json) if src_json else load_trades(args.db)
            st = write_review_state(args.review_state, full, args.label,
                                    digest, summary, reviewed=len(t), slots=slots)
            print(f"\nВодяной знак обновлён: {args.review_state} "
                  f"(последняя сделка id={st['last_trade_id']}, "
                  f"разобрано {st['trades_reviewed']}, {st['config_summary']})")

    if args.no_write:
        return

    os.makedirs(args.out, exist_ok=True)
    enriched_path = os.path.join(args.out, "trades_enriched.csv")
    t.to_csv(enriched_path, index=False)

    m.insert(0, "snapshot_at", datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    m.insert(1, "git_rev", git_rev())
    m.insert(2, "label", args.label)
    m.insert(3, "date_from", t.trading_date.min())
    m.insert(4, "date_to", t.trading_date.max())

    metrics_path = os.path.join(args.out, "metrics.csv")
    header = not os.path.exists(metrics_path)
    m.to_csv(metrics_path, mode="a", header=header, index=False)
    print(f"\nЗаписано: {enriched_path}")
    print(f"Дописан снимок в: {metrics_path}")


if __name__ == "__main__":
    main()
