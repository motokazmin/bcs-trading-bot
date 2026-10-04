"""Дописывает решения в журнал: только вперёд, без перезаписи.

echo '{"id":"p000","action":"long","stop":99.6,"target":100.8,"setup":"pullback","note":"…"}' | python3 decide.py
Пачка — по строке на решение. skip: {"id":"p001","action":"skip","note":"…"}
"""
import json
import sys

from common import journal_path, load_journal, load_points


def main():
    pts, jr = load_points(), load_journal()
    order = sorted(pts)
    nxt = len(jr)
    lines = []
    for raw in sys.stdin:
        raw = raw.strip()
        if not raw:
            continue
        d = json.loads(raw)
        if d["id"] in jr:
            sys.exit(f"{d['id']}: уже в журнале — решения не переписываются")
        if d["id"] != order[nxt]:
            sys.exit(f"{d['id']}: ожидалась {order[nxt]} — решения строго по порядку")
        if d["action"] in ("long", "short"):
            s, t = d["stop"], d["target"]
            ok = s < 100 < t if d["action"] == "long" else t < 100 < s
            if not ok:
                sys.exit(f"{d['id']}: стоп {s} и цель {t} не по разные стороны от 100 для {d['action']}")
        elif d["action"] != "skip":
            sys.exit(f"{d['id']}: action {d['action']!r}")
        lines.append(json.dumps(d, ensure_ascii=False))
        jr[d["id"]] = d
        nxt += 1
    with open(journal_path(), "a") as f:
        for l in lines:
            f.write(l + "\n")
    print(f"записано {len(lines)}, всего {len(jr)}")


if __name__ == "__main__":
    main()
