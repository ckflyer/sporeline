# Sporeline

A cultivation log that runs on your own machine and keeps its data there.
Track spores, mycelium, spawn and grows, see how everything descends from
everything else, and keep your recipes in the same place.

No account, no server, no internet. One executable and one folder.

## Credit

Sporeline follows the design of **[mycolog](https://codesoap.github.io/mycolog/)
by Richard Ulmer**, which is MIT licensed. The four kinds of component, keeping
things you no longer own instead of deleting them, the family tree rules, and
the label alphabet that skips I, O and 0 are all his ideas.

This is a separate program rather than a fork — none of his code is in it — but
it would not exist without mycolog. If you want a smaller tool backed by SQLite,
use his. See [NOTICE](NOTICE) for the full attribution.

## Running it

Download `sporeline.exe` and double-click it. A console window opens and your
browser opens to the log. Leave the console window open while you work; closing
it stops Sporeline.

Windows will warn you about an unrecognised publisher, because the file is not
signed. More info → Run anyway.

```
sporeline.exe -port 9000      use a different port
sporeline.exe -headless       do not open a browser
sporeline.exe -data D:\myco   keep the data somewhere else
```

## Where your data lives

```
C:\Users\<you>\sporeline\
    sporeline.json    your whole log
    pics\             your pictures, full size
```

Copy that folder and you have copied everything. There is also a one-file
backup button on the Data page.

On macOS the folder is `~/Library/Application Support/sporeline`, on Linux
`~/.local/share/sporeline`.

## IDs

Six characters: kind, generation, then three random.

| Prefix | Kind |
| --- | --- |
| `SE` | Spores |
| `MY` | Mycelium |
| `SN` | Spawn |
| `GR` | Grows |

`MY2K7P` is mycelium, second generation, `K7P`. Generation 0 is anything you did
not make yourself. Past nine the generation becomes a letter, so an ID is always
six characters. The random part skips I, O and 0 — the characters you misread off
a piece of tape six weeks later.

## Coming from mycolog

```
python convert_mycolog.py "C:\Users\<you>\mycolog"
```

Then in Sporeline: **Data → Import** and pick the file it wrote.

Your existing mycolog IDs are carried over exactly, so labels already on your
jars stay correct. Only entries you create in Sporeline get the new style. The
converter reads your mycolog folder and changes nothing in it.

## Building

Pure Go, standard library only, no cgo.

```
go build .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o sporeline.exe .
```

## License

Add your own — MIT keeps it compatible with the project this one learned from.
