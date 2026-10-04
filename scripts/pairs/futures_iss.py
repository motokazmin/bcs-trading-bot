"""Дневная история фьючерсов на акции с MOEX ISS — для пар на фьючерсах (обе ноги — фьючерсы).

python3 scripts/pairs/futures_iss.py discover   # список контрактов по датам → data/futures-d/contracts.csv
python3 scripts/pairs/futures_iss.py fetch      # история каждого контракта → data/futures-d/<SECID>.csv

Код истёкшего контракта в ISS повторяется раз в 10 лет: старый получает суффикс года (SRZ6_2016),
поэтому контракты собираются со списков торгов по датам, а не конструируются из тикера.
"""
import csv
import json
import os
import sys
import time
import urllib.request
from datetime import date, timedelta

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "data", "futures-d")
BASE = "https://iss.moex.com/iss/history/engines/futures/markets/forts/securities"
START, END = date(2014, 10, 1), date(2026, 9, 30)
COLS = "TRADEDATE,SECID,OPEN,LOW,HIGH,CLOSE,SETTLEPRICE,VALUE,VOLUME,OPENPOSITION,SHORTNAME,ASSETCODE"
# Базовые активы — акции (коды ISS); индексы, валюты, товары и вечные фьючерсы не берутся.
STOCKS = {
    "SBRF", "SBPR", "GAZR", "LKOH", "ROSN", "VTBR", "GMKN", "NOTK", "TATN", "TATP", "SNGR", "SNGP",
    "MTSI", "MGNT", "ALRS", "AFLT", "MOEX", "PLZL", "CHMF", "NLMK", "MAGN", "HYDR", "FEES", "RTKM",
    "TRNF", "AFKS", "IRAO", "PIKK", "POLY", "PHOR", "RUAL", "YNDF", "YDEX", "OZON", "TCSI", "T",
    "FIVE", "X5", "POSI", "SMLT", "SIBN", "BSPB", "VKCO", "MTLR", "FESH", "CBOM", "SGZH", "SPBE",
    "SFIN", "BELU", "ISKJ", "WUSH", "HEAD", "ASTR", "SOFL", "LEAS", "MVID", "UPRO", "RNFT", "KMAZ",
}


def get(url):
    for attempt in range(5):
        try:
            with urllib.request.urlopen(url, timeout=60) as r:
                return json.load(r)
        except Exception as e:  # сеть ISS иногда отваливается — повтор с паузой
            print(f"повтор {attempt + 1}: {e}", file=sys.stderr)
            time.sleep(3 * (attempt + 1))
    raise RuntimeError(url)


def paged(url):
    rows, start = [], 0
    while True:
        d = get(f"{url}&start={start}")
        h = d["history"]
        rows += [dict(zip(h["columns"], r)) for r in h["data"]]
        idx, total, size = d["history.cursor"]["data"][0]
        start = idx + size
        if start >= total:
            return rows
        time.sleep(0.1)


def discover():
    os.makedirs(OUT, exist_ok=True)
    seen = {}
    d = START
    while d <= END:  # раз в месяц: квартальный контракт торгуется дольше, его не пропустить
        for shift in range(5):  # ближайший торговый день
            day = d + timedelta(days=shift)
            rows = paged(f"{BASE}.json?date={day}&iss.only=history,history.cursor"
                         f"&history.columns=SECID,SHORTNAME,ASSETCODE")
            if rows:
                break
        for r in rows:
            if r["ASSETCODE"] in STOCKS:
                seen[r["SECID"]] = (r["SHORTNAME"], r["ASSETCODE"])
        print(day, len(rows), "контрактов на акции всего", len(seen), flush=True)
        d = date(d.year + (d.month == 12), d.month % 12 + 1, 1)
    with open(os.path.join(OUT, "contracts.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["SECID", "SHORTNAME", "ASSETCODE"])
        for s, (n, a) in sorted(seen.items()):
            w.writerow([s, n, a])


def fetch():
    with open(os.path.join(OUT, "contracts.csv")) as f:
        contracts = list(csv.DictReader(f))
    for i, c in enumerate(contracts):
        path = os.path.join(OUT, c["SECID"] + ".csv")
        if os.path.exists(path):
            continue
        rows = paged(f"{BASE}/{c['SECID']}.json?iss.only=history,history.cursor&history.columns={COLS}")
        rows = [r for r in rows if r["SHORTNAME"] == c["SHORTNAME"]]  # код мог принадлежать другому году
        with open(path, "w", newline="") as f:
            w = csv.DictWriter(f, fieldnames=COLS.split(","))
            w.writeheader()
            w.writerows(rows)
        print(f"{i + 1}/{len(contracts)} {c['SECID']} {c['SHORTNAME']}: {len(rows)} дней", flush=True)
        time.sleep(0.1)


if __name__ == "__main__":
    {"discover": discover, "fetch": fetch}[sys.argv[1]]()
