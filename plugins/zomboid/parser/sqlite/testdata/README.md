# multipage.db

Generated with Python 3's standard-library SQLite module:

```sh
python3 - <<'PY'
import pathlib
import sqlite3
p = 'plugins/zomboid/parser/sqlite/testdata/multipage.db'
pathlib.Path(p).unlink(missing_ok=True)
c = sqlite3.connect(p)
c.execute('pragma page_size=1024')
c.execute('create table t (id INTEGER PRIMARY KEY, name TEXT, blob BLOB, f REAL, n INTEGER)')
for i in range(320):
    n = None if i % 5 == 0 else -i
    if i == 301:
        n = 1 << 40
    if i == 302:
        n = 1 << 55
    c.execute('insert into t values (?,?,?,?,?)', (
        i,
        '' if i % 10 == 0 else 'row' + str(i),
        b'' if i % 11 == 0 else (b'x' * 3000 if i == 100 else b'y' * 20000 if i == 200 else bytes([i % 256])),
        i / 3.0 if i % 7 else None,
        n,
    ))
c.commit()
c.close()
PY
```

The database contains 320 rows, including NULLs, negative integers, 6- and 8-byte
integers, floats, empty TEXT/BLOB values, and 3,000- and 20,000-byte overflow BLOBs.
