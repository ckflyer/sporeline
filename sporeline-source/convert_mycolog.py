#!/usr/bin/env python3
"""
Turn a mycolog data folder into a file Sporeline can import.

Usage:
    python convert_mycolog.py  path/to/mycolog  [output.json]

The mycolog folder is the one holding mycolog.sqlite3 and, if you have
pictures, a pics folder. On Windows it is usually:
    C:\\Users\\<you>\\mycolog

What it does:
  * every component becomes a Sporeline entry of the same kind
  * generations are worked out from the parent links
  * new six character IDs are issued (SE0K7P style), and the old mycolog
    token is kept on each entry so searching for it still finds things
  * yields convert from milligrams to grams
  * the yield comment becomes genetic remarks
  * "gone" stays gone
  * pictures ride along inside the file, base64 encoded

Nothing in your mycolog folder is changed.
"""

import base64
import json
import os
import random
import sqlite3
import sys

KIND = {"SPORES": "spores", "MYC": "myc", "SPAWN": "spawn", "GROW": "grow"}
PREFIX = {"spores": "SE", "myc": "MY", "spawn": "SN", "grow": "GR"}
CHARS = "ABCDEFGHJKLMNPQRSTUVWXYZ123456789"  # no I, O or 0


def gen_char(gen):
    if gen <= 9:
        return str(gen)
    return chr(ord("A") + min(gen - 10, 25))


def new_id(kind, gen, taken):
    prefix = PREFIX.get(kind, "XX") + gen_char(gen)
    while True:
        cand = prefix + "".join(random.choice(CHARS) for _ in range(3))
        if cand not in taken:
            taken.add(cand)
            return cand


def convert(folder, out_path):
    db_path = os.path.join(folder, "mycolog.sqlite3")
    if not os.path.exists(db_path):
        sys.exit("No mycolog.sqlite3 in %s" % folder)
    db = sqlite3.connect(db_path)

    rows = list(db.execute(
        "SELECT id, type, species, token, createdAt, notes, gone FROM component"))
    relations = list(db.execute("SELECT parent, child FROM relation"))
    grows = {}
    try:
        for gid, yield_mg, comment in db.execute(
                "SELECT id, yield, yieldComment FROM grow"):
            grows[gid] = (yield_mg, comment)
    except sqlite3.OperationalError:
        pass  # older mycolog without the grow table

    parents = {}
    for parent, child in relations:
        parents.setdefault(child, []).append(parent)

    # Generation = one past the deepest parent, resolved iteratively so
    # the order rows come out of the database does not matter.
    gen = {}

    def depth(cid, seen=None):
        if cid in gen:
            return gen[cid]
        seen = seen or set()
        if cid in seen:
            return 0
        seen.add(cid)
        ps = parents.get(cid, [])
        value = 1 + max((depth(p, seen) for p in ps), default=-1) if ps else 0
        gen[cid] = value
        return value

    for row in rows:
        depth(row[0])

    taken, id_map, components = set(), {}, []
    for old_id, ctype, species, token, created, notes, gone in rows:
        kind = KIND.get(ctype, "myc")
        g = gen.get(old_id, 0)
        new = new_id(kind, g, taken)
        id_map[old_id] = new
        comp = {
            "id": new,
            "kind": kind,
            "species": species or "Unnamed",
            "gen": g,
            "created": (created or "")[:10],
            "notes": notes or "",
            "gone": bool(gone),
            "parents": [],
            "legacy": token or "",
            "added": 0,
        }
        if old_id in grows:
            yield_mg, comment = grows[old_id]
            if yield_mg:
                comp["yield"] = round(yield_mg / 1000.0, 2)  # mg -> g
            if comment:
                comp["remarks"] = comment
        components.append((old_id, comp))

    by_old = dict(components)
    for old_id, comp in components:
        comp["parents"] = [id_map[p] for p in parents.get(old_id, []) if p in id_map]

    # Pictures: mycolog names them <component-id>_<n>.<ext>. Rename to the
    # new ID and carry the bytes along inside the file.
    pictures = {}
    pics_dir = os.path.join(folder, "pics")
    if os.path.isdir(pics_dir):
        for name in sorted(os.listdir(pics_dir)):
            stem, _, ext = name.rpartition(".")
            old, _, pos = stem.partition("_")
            try:
                old = int(old)
            except ValueError:
                continue
            if old not in id_map:
                continue
            new_name = "%s_%s.%s" % (id_map[old], pos or "0", ext)
            with open(os.path.join(pics_dir, name), "rb") as fh:
                pictures[new_name] = base64.b64encode(fh.read()).decode("ascii")
            by_old[old].setdefault("pics", []).append(new_name)

    bundle = {
        "version": 1,
        "components": [c for _, c in components],
        "recipes": [],
        "pictures": pictures,
    }
    with open(out_path, "w") as fh:
        json.dump(bundle, fh, indent=2)

    gone = sum(1 for _, c in components if c["gone"])
    print("Converted %d cultures (%d marked gone), %d pictures." %
          (len(components), gone, len(pictures)))
    print("Wrote %s" % out_path)
    print("In Sporeline: Data -> Import -> choose this file.")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    folder = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else "sporeline-from-mycolog.json"
    convert(folder, out)
