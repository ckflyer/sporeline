# Sporeline

Sporeline is a cultivation log for mushroom growers. It keeps track of your
spores, mycelium, spawn and grows, and of how each one came from the one
before it, so you can look at any jar and see exactly where it came from.

It runs on your own computer and keeps everything in a folder you control.
There is no account, no server and no internet connection.

- Four kinds of entry: spores, mycelium, spawn, grows
- A family tree on every culture, showing its parents, siblings and everything
  made from it
- Short printable IDs so the label on the jar matches the entry on screen
- Recipes that scale to whatever batch size you need
- Yields and genetic remarks on grows
- Contamination tracking, with a statistics page showing where in the process
  you lose things
- Pictures, daily backups, and an importer for mycolog

## Getting it running

You do not need to know anything about command prompts or programming.

1. Go to the Releases page of this repository and download `sporeline.exe`.
2. Put it wherever you like: your Desktop, or a folder in Documents. It does
   not install anything and does not need admin rights.
3. Double-click it.
4. Windows will probably show a blue box saying "Windows protected your PC".
   This happens with any program that has not been signed by a registered
   company, which costs a few hundred dollars a year. Click **More info**, then
   **Run anyway**.
5. A black window appears and your browser opens to Sporeline. That is it.

Leave the black window open while you are using Sporeline. It is the program
itself. Closing it shuts Sporeline down, and everything you entered is already
saved. To start again later, double-click the file again.

If your browser does not open on its own, look at the black window. It prints
an address like `http://localhost:8099`. Type that into your browser.

Some antivirus programs are also suspicious of new, unsigned programs. If yours
quarantines the file, you will need to allow it.

## Where your data lives

```
C:\Users\<your name>\sporeline\
    sporeline.json    your log
    pics\             your pictures
    backups\          dated copies of your log
```

Copy that folder and you have copied everything. Deleting or replacing
`sporeline.exe` does not touch it, so upgrading is just swapping the file.

On a Mac the folder is `~/Library/Application Support/sporeline`, and on Linux
`~/.local/share/sporeline`.

## Backups

Sporeline copies your log into the `backups` folder once a day when it starts,
and always before an import or a restore. It keeps the last thirty. You can
change that, restore an earlier copy, or save a single backup file from the
**Data & settings** page.

## IDs

Every entry gets a six-character ID. The first two letters say what it is, the
next character is the generation, and the last three tell it apart from
everything else.

| Prefix | Kind |
| --- | --- |
| `SE` | Spores |
| `MY` | Mycelium |
| `SN` | Spawn |
| `GR` | Grows |

So `MY2K7P` is mycelium, second generation. Generation 0 is anything you did not
make yourself. The random characters never include I, O or 0, because those are
the ones you misread off a piece of tape six weeks later.

When you add cultures, Sporeline shows you the IDs it is about to use before you
save, so you can write them on the jars while you work.

## Coming from mycolog

Close mycolog first. Then in Sporeline go to **Data & settings**, find Import,
choose "A mycolog folder", and give it the folder mycolog uses, usually
`C:\Users\<your name>\mycolog`.

Everything comes across: cultures, lineage, notes, yields and pictures. Your IDs
stay exactly as they were, so labels already on your jars still match. The
mycolog folder is only read, never changed, and Sporeline backs up its own log
before it starts.

## Options

Sporeline takes a few switches if you want them. Most people never will.

```
sporeline.exe -port 9000      use a different port
sporeline.exe -headless       do not open a browser
sporeline.exe -data D:\myco   keep the data somewhere else
```

## Building from source

Sporeline is written in Go with nothing but the standard library. No database
driver, no C compiler, no build step for the front end.

```
go build .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o sporeline.exe .
```

The Windows icon comes from `rsrc_windows_amd64.syso`, which the Go toolchain
picks up on its own. To rebuild it after changing `assets/sporeline.ico`:

```
go install github.com/akavel/rsrc@latest
rsrc -ico assets/sporeline.ico -arch amd64 -o rsrc_windows_amd64.syso
```

## Credit

Sporeline is heavily inspired by [mycolog](https://codesoap.github.io/mycolog/)
by Richard Ulmer. It is a separate program and contains none of his code, but it
would not exist without his. If you want something smaller and simpler, use his.

## License

MIT. Use it, change it, share it, build on it, for anything you like. The only
condition is that the copyright notice stays with it. See [LICENSE](LICENSE).
