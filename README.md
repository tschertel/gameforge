# GameForge

[Português (Brasil)](README.pt-BR.md)

**Compress, verify and organize backups of the games you own**: Nintendo Switch, disc-based consoles and cartridge ROMs, in one small desktop app for Windows and Linux (macOS experimental). There is nothing to install, it is one file, and it needs no other tools.

[![Latest release](https://img.shields.io/github/v/release/tschertel/gameforge)](https://github.com/tschertel/gameforge/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

![The Nintendo Switch tab](docs/images/en/switch.png)

Current version: **2.23.0**. [Download](#download) · [Full guide](docs/GUIDE.md) · [What's new](CHANGELOG.md)

> **New in 2.21: Collection report.** Point it at your `ROMS` folder and get one HTML page of all your games, by console, with search and filters. It shows the games you have on more than one console, such as *LEGO Star Wars II* on PS2, PSP, GBA and Xbox, or *Street Fighter Alpha 3* on CPS2 (`sfa3.zip`) and PS1. In the ROMs tab: **Check ▾ → Collection report**.

![The collection report, showing the games on more than one console](docs/images/en/collection.png)

## What it does

### Nintendo Switch

- **Merges** the base game, its update and its DLCs (NSP, NSZ or XCI) into **one file**.
- **Compresses to NSZ** while merging, usually 30–50% smaller. Installers rebuild the original files byte for byte. It also decompresses NSZ back to NSP.
- **Checks backups**, one or a whole library at a time: every file is hashed and compared with what the package says it holds.
- **Updates and DLCs ▾** compares your games with an online database, to tell which updates or DLCs are missing, and can save that list as a CSV file.
- **Renames** files to `Name [TitleID][vVersion].nsp`.

It needs your own `prod.keys` in the `keys` folder.

### Discs

![The Discs tab](docs/images/en/discs.png)

For PS1, PS2, PSP, Saturn, Sega CD, Dreamcast, GameCube, Wii, Xbox and more:

- **Converts** CUE/BIN, GDI and ISO to **CHD**, also from `.bin.ecm`, Nero `.nrg`, DiscJuggler `.cdi` and ZIP or 7z files, without unpacking by hand; ISOs to **CSO/ZSO**, GameCube and Wii discs (also Wii `.wbfs`) to **RVZ**, and Xbox discs to **XISO**. **Extract** turns CHDs back into CUE/BIN, GDI or ISO.
- **Shows what each disc is**: product code, region, version, and the icon or disc label.
- **Renames** discs with their **Redump** names.
- **Checks RetroAchievements**: whether your version of each game has achievements.

### ROMs

![The ROMs tab](docs/images/en/roms.png)

- **Compresses** ROMs to **7z** or **ZIP** (for emulators that crash on 7z), one archive per game; recompresses arcade ZIPs to 7z, and turns 7z files into ZIPs.
- **Renames** ROMs with their **No-Intro** names, finds **duplicates** and keeps **one version per game** (1G1R) by your preferred regions.
- **Collection report** (new): an HTML page of the games in your console folders, with a search field and filters, that shows which games you have on more than one console, arcade sets included.
- **Checks arcade sets** for FBNeo, the RetroArch MAME cores and Flycast (NAOMI, Atomiswave), and tells the AES and MVS versions of Neo Geo games apart.
- **Checks RetroAchievements**. A click on the status of an unsupported version opens the list of the versions RetroAchievements accepts, and each check can save an HTML report.
- **Tools** trim Nintendo DS ROMs, convert Nintendo 64 ROMs to `.z64` and Mega Drive `.smd` files to `.md`, and remove SNES and PC Engine copier headers. **Check ▾ → Header checksums** tells whether N64, SNES, Mega Drive and DS ROMs match the checksum in their header.

### Nice touches

![The question about updating old lists](docs/images/en/lists-question.png)

- **Keeps your lists fresh**: if you agree, the app looks at startup for the name lists you downloaded that have grown old and asks before updating them. Lists you never used are never downloaded.
- **Tells you about new versions**, with a link at the top of the window. It asks first, and sends only the app's version and your system, which the project's server counts to know roughly how many people use the app ([what is sent and kept](server/README.md)).
- **Remembers where you were**: each tab keeps its own output folder, and the last tab, the window size and the columns come back on the next start.
- **Renames the RetroArch way**: saves, save states and thumbnails named after a game are renamed with it, and the `.m3u` playlists of games on several discs are updated.
- **Shows your games**: icons of DS, PSP, GameCube and Xbox games and the labels of Dreamcast discs, right in the lists.
- **Drag and drop** files or folders onto the window (Windows and Linux): each file goes to the tab of its format.

## Download

Get the file for your system from [Releases](https://github.com/tschertel/gameforge/releases/latest), unzip it and run it:

| System | File |
|---|---|
| Windows (64-bit) | `gameforge-X.Y.Z-windows-amd64.zip` → `gameforge.exe` |
| Linux (64-bit) | `gameforge-X.Y.Z-linux-amd64.tar.gz` → `./gameforge` |
| macOS (experimental, untested) | `gameforge-X.Y.Z-macos-arm64.tar.gz` (Apple Silicon) or `-amd64` (Intel) |

For Switch games, put your `prod.keys` in the `keys` folder next to the program. For Wii discs, also put the Wii common key there. Disc images and ROMs need no keys.

## Good to know

- **Careful with your files**: results are written under a temporary name and **verified after writing**. Renaming shows the changes first and never writes over a file. An original is replaced or removed only by an option or tool that says so, after the new file has been read back.
- **The name databases** (Redump, No-Intro, arcade sets, RetroAchievements) are downloaded the first time they are needed, after asking, and kept in a `cache` folder.
- **English, Portuguese, German, French and Spanish**, with light and dark themes.
- **No games and no keys** are included: use it only with games you own and keys from your own console.

## More

- **[Full guide](docs/GUIDE.md)**: every option, file formats, settings and limitations.
- **[Building from source](docs/GUIDE.md#building)**: Go, without a C compiler.
- **[Changelog](CHANGELOG.md)**.
- Developed with **[Ponytail](https://github.com/DietrichGebert/ponytail)**.

## License

[MIT](LICENSE). Third-party licenses are in [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt). The Redump and No-Intro databases are downloaded by the app and are not part of this repository.
