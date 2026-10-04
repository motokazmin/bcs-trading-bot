"""Общее для слепой ручной торговли по графикам (docs/analysis/0016).

Точки решения — случайные (тикер, момент) из истории. Решение принимается по картинке,
на которой только прошлое: бары, закончившиеся не позже момента решения. Тикер и даты
скрыты, цены нормированы к последнему close = 100 — чтобы знание реальной истории рынка
не подсказывало будущее. Исход считается только после записи решения в журнал.
"""
import json
import os
from datetime import datetime, timedelta, timezone

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HIST = os.path.join(ROOT, "data", "history")
WORK = os.environ.get("TA_BLIND_DIR") or os.path.join(ROOT, "results", "ta-blind")
TICKERS = ["AFKS", "CHMF", "GAZP", "LKOH", "MGNT", "MOEX", "NVTK", "ROSN", "SBER", "TATN"]
MSK = timezone(timedelta(hours=3))
BAR = pd.Timedelta(minutes=5)

# Основная сессия МСК: решения 10:30…17:00, принудительный выход — по close бара, кончающегося в 18:40.
DECIDE_FROM, DECIDE_TO = "10:30", "17:00"
EOD = "18:40"

# Издержки как у бота: комиссия 0.008% на ногу, проскальзывание 1 б.п. на ногу.
COMMISSION = 0.00008
SLIPPAGE = 0.0001

_cache = {}


def load(ticker):
    if ticker not in _cache:
        df = pd.read_csv(os.path.join(HIST, ticker + ".csv"))
        df["ts"] = pd.to_datetime(df["timestamp"], utc=True).dt.tz_convert(MSK)
        df["end"] = df["ts"] + BAR
        df = df.drop(columns=["timestamp"]).sort_values("ts").reset_index(drop=True)
        _cache[ticker] = df
    return _cache[ticker]


def points_path():
    return os.path.join(WORK, "points.json")


def journal_path():
    return os.path.join(WORK, "decisions.jsonl")


def load_points():
    with open(points_path()) as f:
        return {p["id"]: p for p in json.load(f)}


def load_journal():
    out = {}
    if os.path.exists(journal_path()):
        for line in open(journal_path()):
            line = line.strip()
            if line:
                d = json.loads(line)
                out[d["id"]] = d
    return out


def past(ticker, t):
    """Только бары, закончившиеся не позже t. Единственный доступ к данным при отрисовке."""
    df = load(ticker)
    return df[df["end"] <= t]
