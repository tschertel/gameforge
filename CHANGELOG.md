# Changelog

All notable changes to GameForge. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/): the minor number grows with new features, the patch number with small fixes.

## [2.23.5] - 2026-11-27

### Fixed
- The Discs tab reads the game of self-booting Dreamcast CD-Rs (DiscJuggler `.cdi` images and the CHDs made from them), whose first data track holds no game: the Platform and Details columns show it, as for other discs.
- CHDs made by `chdman` with the Zstandard codecs (`cdzs` for CDs and GD-ROMs, `zstd` for DVDs) are read: before, listing, checking, extracting and the RetroAchievements check stopped with `unsupported codec "cdzs"`.
- The RetroAchievements check reads Dreamcast CDs whose data track comes after audio tracks or a first session (for example, Sturmwind); before, it stopped with `sector … is outside track …`.

## [2.23.4] - 2026-11-27

### Fixed
- On Linux, the columns of the lists fit their longest text again (Tk 9 gives a measure there that broke the fitting without a message), so long file names and platforms such as `Game Boy Advance` are no longer cut off. The Platform column widens too.
- A damaged NSZ whose block count does not fit in the file is refused as invalid; before, the app could ask for gigabytes of memory and close.
- Errors of the interface's Tcl commands are written to the terminal instead of being dropped.

### Changed
- The update server counts only checks of versions from 2.23.0 to the latest release, so made-up versions are not counted.

## [2.23.3] - 2026-11-27

### Changed
- The **Platform** column of the ROMs tab shows the console of the ROMs inside 7z and ZIP files (`SNES`, `Mega Drive`…) instead of `7z` or `ZIP`. Arcade sets found in the downloaded arcade lists show `Arcade`, or `Naomi`, `Naomi 2`, `Atomiswave` or `Sega System SP` for Flycast's games.
- The RetroAchievements report has a search box and a drop-down that shows only the files with problems (another version supported, version not recognized, not checked), one of those results, or the recognized ones.
- In the ROMs tab, **Save an HTML report of each RetroAchievements check** moved from **Names database ▾** to **Check ▾**, under **RetroAchievements**.

## [2.23.2] - 2026-11-27

### Fixed
- The ROMs tab shows the details of a ZIP file like those of a 7z file (`ZIP · 1 file(s) · 25.44 MiB`), instead of with commas.

## [2.23.1] - 2026-11-26

### Fixed
- Changing **Archive: 7z or ZIP** in the ROMs tab keeps the files you unchecked unchecked; before, they were checked again and compressed with the others.
- Opening a folder with thousands of `.bin` ROMs in the ROMs tab no longer reads the folder once per file to look for a CUE or GDI sheet.
- The collection report names arcade sets by the same title each time (the one the ROMs tab shows), instead of the title of whichever list came first.
- **Export what is missing (CSV)…** separates the columns with semicolons when the app is not in English, as Excel expects in those regions; before, Excel showed each row in a single column there.

## [2.23.0] - 2026-11-26

### Changed
- The check for new versions asks the project's own server (a Cloudflare Worker, whose code is in [`server/`](server/README.md)) instead of GitHub, and the server counts the checks per day, app version and system, to know roughly how many people use the app. It is sent the app's version, the system and the processor, nothing else; it keeps only those counts, not IP addresses. If it does not answer, the app asks GitHub as before. The question at startup says exactly what is sent and kept, and whoever turned the check on with an older version is asked again, once; whoever said no is not.

## [2.22.0] - 2026-11-26

### Added
- **Check backup ▾** checks many Switch backups at once: the files chosen in the dialog or the files checked in the list, with a summary at the end that names those with problems ([#4](https://github.com/tschertel/gameforge/issues/4)).
- **Updates and DLCs ▾ → Export what is missing (CSV)…** saves, one row per game, the missing update and the missing and outdated DLCs ([#4](https://github.com/tschertel/gameforge/issues/4)).
- **Save the log of each run to a file**, in the About window, keeps the log of each run as a dated text file in the `logs` folder; off by default ([#4](https://github.com/tschertel/gameforge/issues/4)).
- **Archive: 7z or ZIP** in the ROMs tab. ZIP archives are larger but open in emulators that list 7z and crash on it; with ZIP, 7z files in the list become ZIPs with the same files and dates. Thanks to the macOS user on Reddit who asked for it.

### Changed
- The ROMs tab lists only games: cartridge ROMs of the consoles it knows and ZIP and 7z archives, no longer saves, states or images. The extension checkboxes below the list are gone: the files checked in the list are the ones compressed, and each row tells what it becomes (`→ 7z`, `→ ZIP`, or that it stays as it is). A `.bin` next to a CUE or GDI sheet is a disc track and is not listed.

### Fixed
- Drop-down lists can no longer be typed into after a run.
- On macOS, the descriptions at the top of the tabs no longer make the window grow or change size again and again: a description no longer asks for more room than the window gives it ([#3](https://github.com/tschertel/gameforge/pull/3), by @mbisono).
- On macOS, the automatic theme follows the system's dark mode, and the window's own parts (title bar, native controls) follow the theme chosen ([#3](https://github.com/tschertel/gameforge/pull/3), by @mbisono).

## [2.21.1] - 2026-11-25

### Changed
- The ROMs tab lists arcade sets by their titles from the downloaded arcade lists, with the file name in brackets: `Street Fighter Alpha 3 (USA 980904) [sfa3.zip]`.

## [2.21.0] - 2026-11-25

### Added
- **Check ▾ → Collection report (games on several consoles)…** in the ROMs tab lists the games in a folder of console folders as an HTML page, with a search field, a console filter and a filter for the games you have on more than one console. Arcade sets named by their short names (`sfa3.zip`) count by their titles from the arcade lists.

## [2.20.3] - 2026-11-24

### Fixed
- An NSZ whose compressed data is cut off (a solid NCZ, for example from an interrupted download) is reported as corrupt; before, it became a shorter NCA in the NSP and the verification still passed.
- `gameforge.json` is saved to a temporary file that then replaces it, so an interrupted save no longer leaves a broken file whose settings were then reset; a file that cannot be read is kept as `gameforge.json.bad`. On Linux and macOS the file is now readable by its owner only also when an older version created it.
- Two files with the same output name (`Game.gb` and `Game.zip` both become `Game.7z`) are no longer converted at the same time; the second one is skipped as existing.
- A damaged file that made a reader fail unexpectedly (for example a CHD whose header gives an impossible size) no longer closes the app: that file fails, with its error in the log, and the others go on.
- On Linux, renaming a file to a name that differs only in upper and lower case no longer replaces another file with that name.
- On Windows, a file that cannot go to the Recycle Bin (larger than it, or on a drive without one) is no longer deleted for good without asking: Windows asks first, and No keeps it.
- 1G1R no longer takes the discs of one game for versions of it: `(Disc 1)`, `(Disc 2)`, `(Side B)` and the like are each kept.
- A failed RomFS check of a game with the update applied fails the merge (the file is removed) instead of only being logged.
- 7z archives and the files extracted from CHDs are written as `.part` files and named once complete and checked, so an interrupted run no longer leaves a file that looks complete and is then skipped as existing.
- **Rename…** renames none of the files of a split WBFS image (`.wbfs`, `.wbf1`…) or of its saves when one of the new names is taken, instead of renaming some of them.
- Downloading the list of one console no longer counts the other lists as current: each list keeps its own date, and the oldest one tells whether to offer an update.
- After compressing to 7z with **Delete the originals after verifying**, the list shows the archives instead of the deleted files.
- A damaged CHD, CSO, ZSO, RVZ or 7z file can no longer make the app reserve gigabytes of memory from a wrong size in its header.
- The temporary folder of a disc unpacked from a ZIP or 7z is reported in the log when it cannot be removed, and folders left by earlier runs are removed after a day.

### Changed
- Less duplicated code: the database caches share one loader, the title database uses the common download code, and some functions only the tests used moved into the tests.
- The CHD tests also read a small CHD made by chdman from synthetic data, so they no longer depend only on CHDs this app wrote.

## [2.20.2] - 2026-11-23

### Fixed
- In the Nintendo Switch tab, checking or unchecking files (also with **Select none**) no longer expands every game you collapsed or scrolls the list to another place.

## [2.20.1] - 2026-11-23

### Changed
- Opened and dropped folders add only their own files. The new **Include subfolders** box in the folder dialog of **Open folder…**, off by default, adds the files in their subfolders too, as before; dropped folders follow its last state. **Open folder…** now always uses the app's own folder dialog, which has the box.

## [2.20.0] - 2026-11-23

### Added
- The Discs tab converts Wii **WBFS** images (`.wbfs`, also split into `.wbf1`, `.wbf2`… parts) to RVZ, read as the disc they hold, without writing an ISO first; **Rename…** gives them their Redump names by the game ID and renames the parts with them.

### Fixed
- The title of a GameCube or Wii disc no longer shows the MD5 that some backup tools keep after the title field of the disc header (`MD5#…`).

## [2.19.0] - 2026-11-23

### Added
- **Check ▾ → Header checksums** in the ROMs tab checks the checksum in the header of Nintendo 64 (CRC1/CRC2), SNES, Mega Drive and Nintendo DS ROMs, loose or in a ZIP or 7z, and tells which ROMs were changed (or released with a wrong checksum).
- **Tools ▾** in the ROMs tab converts Mega Drive `.smd` files to `.md` and removes the copier header of SNES (`.smc` becomes `.sfc`) and PC Engine ROMs.
- **Rename…** and the duplicate check recognize Mega Drive `.smd` files and PC Engine ROMs with a copier header, and find ROMs with a header or in another byte order also inside 7z files.
- The Discs tab converts more CD images to CHD: `.bin.ecm` (ECM, decoded on the fly with its checksum checked; also when a `.cue` names the `.bin`), Nero `.nrg` and DiscJuggler `.cdi` images (subchannel data left out, the gap between sessions kept as empty sectors), and ZIP or 7z files holding a disc image, unpacked into a temporary folder next to the output and removed afterwards.
- At startup, the app can ask whether to update the lists you downloaded that have grown old: it asks once whether to do so (also after an update from an older version), and the **Updates** box of the **About…** window turns it on or off (`lists_check`). As often as the check for new versions is set to run, it then asks once whether to update the old lists (ROM and disc names, arcade sets and RetroAchievements lists after 30 days, the Switch game database after 14), naming them. Only those lists are downloaded again; nothing new is downloaded.

### Changed
- **Check CHD…** explains a CHD whose hunks are all intact (each one matches its CRC) but whose header holds a data SHA-1 that is not the SHA-1 of its data, and shows both SHA-1s, instead of only `chd: data SHA-1 mismatch`. Such files turn up in collections (some PS1, Saturn and Dreamcast CHDs) and `chdman verify` fails them the same way: the header was written for other data, or the data was changed after the CHD was made. Their tracks can still be good; extracting them and comparing with Redump tells.
- The HTML report of the RetroAchievements check is now optional and off by default: **Names database ▾ → Save an HTML report of each RetroAchievements check** turns it on, in the Discs and ROMs tabs alike (`ra_report` in the settings).

## [2.18.0] - 2026-11-22

### Added
- When RetroAchievements has a game but not the checked version, its status (`✗ RA: only another version is supported`) now shows the address of the page with the hashes RetroAchievements accepts for the game, and a click on the status opens it in the browser; the pointer becomes a hand over it.
- Each RetroAchievements check saves an HTML report for each console, next to the checked files (in the folder of the first one), named after the console and the day, such as `RetroAchievements - SNES - 2026-11-22.html` (a second check on the same day gets ` (2)`). It lists every file with its result, a link to the game on RetroAchievements or, for a version not supported, to the hashes it accepts, and its hash. The log tells where the report is.

### Changed
- The description below the buttons of each tab wraps at the window's width, instead of a fixed width, so a wide window shows it in fewer lines and leaves more room for the list.

## [2.17.1] - 2026-11-22

### Fixed
- On Linux, a message shown at the end of a task (such as the Neo Geo check with AES/MVS marking, or **Rename…** when no file needs a new name) could get no input at all: its OK button did nothing, it could not be closed, and the program kept a processor core busy until it was ended. It happened when the message opened while the log was scrolling. Such messages now open only once the window is free to take input.

## [2.17.0] - 2026-11-20

### Added
- **Check ▾ → 1G1R** in the ROMs tab (one game, one ROM): of the versions of each game among the checked files, keeps one by an order of regions (**1G1R, regions** at the bottom of the tab, `region_order` in the settings; by default `World, USA, Europe, Brazil, Japan`), passing over betas, prototypes, demos and bad dumps and preferring the latest revision, then offers to check the other versions for **Delete checked…**. Versions are told by their No-Intro names, found by CRC or taken from the file names.
- **Tools ▾** in the ROMs tab, with the tools for one console. **Nintendo 64: convert checked ROMs to .z64** writes `.v64` and `.n64` ROMs in the cartridge's byte order, as No-Intro lists them, and removes the originals once the new files are read back.
- **Tools ▾ → Nintendo DS**: **trim checked ROMs** cuts the empty space (0xFF) after the game from loose `.nds` files, keeping the Download Play signature and using the DSi size for games with DSi features; **restore checked ROMs to full size** fills them up to their cartridge's size again, giving back the original file exactly. Files are replaced in place after being read back, and a ROM with data after the game is left alone.
- **Rename…** and the duplicate check recognize trimmed DS ROMs, loose or in a ZIP or 7z, by the CRC of the ROM at its cartridge's full size.
- Loose DS ROMs show their game code, banner title and, when trimmed, their cartridge's size in the ROMs tab; loose Nintendo 64 ROMs show their game code, header title, revision and, for `.v64` and `.n64`, their byte order.

## [2.16.0] - 2026-11-20

### Added
- **Extract** in the Discs tab writes the checked CHDs back as the images they were made from: a CD as a `.cue` with one `.bin` per track, named and laid out as Redump's, a Dreamcast GD-ROM as a `.gdi` with its tracks, a DVD (PS2, PSP) or a CD made from an ISO as an `.iso`. A right click on a CD with one data track offers an ISO of its data instead. Existing files are never written over, the CHD is kept, and with **Verify after writing** the files are read back and compared. A CUE/BIN or GDI converted to CHD and extracted again is identical to the original.
- **Rename…** in the Discs tab gives Xbox discs (disc images and XISOs) their Redump names, found by the title ID in `default.xbe` (such as `EA-091`) and the regions it runs in; a disc image's CRC tells versions apart, while an XISO with several versions for its region keeps its name and the log lists them. The Discs tab shows the title, title ID and regions of Xbox games.
- The Discs tab lists `.cso` and `.zso` files, with the game information of the image inside; **Extract** writes them back as an `.iso`, and **Rename…** and the RetroAchievements check work on them.

### Changed
- The **ISO → CSO/ZSO** tab is now part of the Discs tab: **ISO to:** below the list chooses CHD (the default), CSO or ZSO for ISO images, and the convert button is now **Convert**. If the CSO tab was the one shown last, the Discs tab opens with CSO or ZSO chosen and that tab's output folder. The setting `ciso_format` became `iso_format`.
- The **GameCube/Wii → RVZ** tab is now part of the Discs tab: GameCube and Wii disc images there become RVZ, RVZ files are listed for renaming, and the block size and the Wii common key are on a second row of options below the list. If the RVZ tab was the one shown last, the Discs tab opens with that tab's output folder. The window now has three tabs: Nintendo Switch, Discs and ROMs.
- The **Xbox → XISO** tab is now part of the Discs tab too: an Xbox or Xbox 360 disc image there is rebuilt as an XISO, and an XISO is listed for renaming. If the Xbox tab was the one shown last, the Discs tab opens with that tab's output folder.

### Fixed
- The buttons above the list of the Discs tab no longer grow taller when the names database label next to them wraps to several lines; the label is also shorter when only one or two consoles are missing (`names: all but Xbox, data of …`).

## [2.15.0] - 2026-11-19

### Added
- **Check ▾ → Neo Geo .neo games (Geolith)** in the ROMs tab checks the checked `.neo` files, loose or in a ZIP or 7z, against libretro's Neo Geo list by their CRC, and tells from the product code in the list's name whether each is the home version (`AES (NGH)`), the arcade version (`MVS (NGM)`), both (same ROMs) or has no code; files the list does not know show `✗ Neo Geo: unknown CRC`. **Neo Geo: check the MVS versions that have an AES version** and **Neo Geo: check the AES versions that have an MVS version** then check, for **Delete checked…**, the version you do not want of each game the folder has in both; games with one version are never checked.

### Changed
- **Check ▾ → Sets for …** tells a ZIP or 7z holding a `.neo` file (a whole Neo Geo game in one file, for the Geolith core or the NeoSD) apart from an unknown set: the status shows `– FBNeo: .neo file, not an arcade set` instead of `not in the list`, and the log explains why such a file cannot be checked as an arcade set and, if libretro's Neo Geo list is downloaded, whether it knows the file's CRC. **Rename…** checks `.neo` files against that list.

## [2.14.0] - 2026-11-18

### Added
- **Check ▾ → Sets for FBNeo** (and **Sets for MAME 2000**, **2003**, **2003-Plus**, **2010**, **2015** and **2016**, the MAME cores of RetroArch, and **Sets for Flycast (Naomi, Atomiswave)**) in the ROMs tab checks the checked ZIPs and 7z files as arcade sets of that emulator, with its list of sets, downloaded from GitHub the first time it is used (FBNeo's own list, about 14 MB; for the MAME cores, the lists libretro keeps for them, from 2 to 53 MB): whether the list has the set's short name, and whether all its files are there with the right CRCs, in the set itself or in its parent and BIOS sets in the same folder (non-merged, split and merged sets all work). The status column shows `✓ FBNeo: Blazing Star`, or what is wrong: files missing, files with another CRC (usually a set made for another MAME version), or a clone's parent set the folder lacks (`needs blazstar.zip in this folder`). The CHDs a set needs (MAME's disks, the GD-ROMs of Naomi games) are looked for where the emulators look, `set/name.chd` next to the archive, and a missing one counts as missing. Flycast has no list of sets: its table of games (in its source, `naomi_roms.cpp`, about 300 KB) is downloaded and read as data. Only the folder of the ROMs is looked at, and no BIOS is needed: a BIOS set (`neogeo.zip`, `naomi.zip`, `awbios.zip`) in the folder is checked too, and one that is not there is only noted in the log; the game's own files decide. Sets the emulator marks as not working well and archives whose name is unknown but whose files are a known set are pointed out. Only the archives' headers are read; nothing is changed or rebuilt. **Names database ▾** updates the lists already downloaded.

- **Check ▾ → Duplicates: check only the extra copies** in the ROMs tab checks the extra copies found by the last duplicate check at any time, not only when the check offers it, and unchecks all the other files; it runs the check first if there was none or the list changed.

### Changed
- The ROMs tab gathers its checks in one **Check ▾** menu (RetroAchievements, Duplicates, Sets for FBNeo, the MAME cores and Flycast), which keeps its row of buttons narrow enough for a 1366-pixel screen.

## [2.13.1] - 2026-11-17

### Fixed
- On the first run on Linux (seen on KDE Plasma with Wayland), the question whether to check for new versions did not respond: **Yes** and **No** did nothing and the window could not be closed. The question was opened before the window manager had shown the main window; it now waits until the main window is on screen.

## [2.13.0] - 2026-11-17

### Changed
- The **About…** window has an **Updates** box, as in Rufus: a list chooses how often the app checks for a new version (Never, Daily, the default, Weekly or Monthly), and **Check now** asks GitHub at once, even with the automatic check off, and tells whether a newer version is out, this one is the newest, or the check failed. The box also shows when the last check was made.

## [2.12.0] - 2026-11-17

### Added
- **Find duplicates** in the ROMs tab finds the checked ROMs, ZIPs and 7z files that hold the same game, such as a Neo Geo game as a `.neo`, inside a `.7z` and as MAME's `.zip` set. Files count as the same game when the No-Intro database gives them the same name (by the ROM's CRC), when they hold the same ROM, or when they have the same name; MAME's Neo Geo sets are found by their short names (`blazstar.zip` is Blazing Star). The status column tells which file of each game stays and which are extra copies, and the log lists each group with the files' sizes and how much space the extra copies take. The app then offers to check only the extra copies. The file that stays is the first format in an order set in the tab (by default `7z, neo, zip`: a 7z with the `.neo` inside, then a loose `.neo`, then MAME's ZIP), then the one that already has the database name, then the largest.
- **Delete checked…** in the ROMs tab deletes the checked files, after showing them and asking: to the Recycle Bin on Windows, for good on Linux and macOS. RetroArch saves and states are left alone.
- Experimental macOS builds, for Apple Silicon and Intel Macs, compiled from the same code but not tested on a Mac. They are not signed by Apple; the README explains how to open them.

## [2.11.1] - 2026-11-17

### Fixed
- **Rename…** in the Nintendo Switch tab renamed every file in the list, checked or not; it now renames only the checked files. The same goes for **Rename…** and **Check RetroAchievements** in the Discs, GameCube/Wii → RVZ and ROMs tabs, and for the online check with the game database, which looks only at games with a checked file. When none of the files an action works on is checked, the app says so.

### Changed
- CHDs in the Discs tab, RVZs in the RVZ tab and 7z files in the ROMs tab, which are listed to be renamed or checked but not converted, now have a check box like the other files, checked at first.
- The file lists scroll sideways: each column is at least as wide as its longest text, and a horizontal scroll bar shows what does not fit in the window.

## [2.11.0] - 2026-11-17

### Added
- **Rename…** in the ROMs tab also renames Neo Geo games in the single-file `.neo` format (NeoSD, MiSTer, Geolith), loose or in a ZIP or 7z, with the names of libretro's own Neo Geo list (`dat/SNK - Neo Geo.dat`), downloaded on demand like the No-Intro lists: `sonicwi2.neo` → `Aero Fighters 2 - Sonic Wings 2.neo`. Western and Japanese titles are joined by " - ". MAME and FBNeo zip sets still keep their short names.
- **Files at a time** in the ROMs tab compresses several files at once (1 by default; half the processor's threads, up to 4, are recommended). The progress bar shows the whole batch, and error lines name their file.
- The app can check once a day on GitHub whether a newer version is out. It asks the first time it starts; when there is a newer version, a link to it appears at the top of the window. Only the app's name and version are sent, and nothing is downloaded or installed. The check can be turned on or off in **About…**.

### Changed
- The databases are downloaded from the root of libretro-database, since the Neo Geo list is in another folder than the No-Intro and Redump lists.

## [2.10.0] - 2026-11-16

### Added
- Nintendo DS ROMs show their icon (the 32 × 32 picture of the DS menu, from the ROM's banner) next to their name in the ROMs tab, loose or in a ZIP or 7z; in a 7z only the start of the ROM is decompressed, in the background, so the list appears at once and the icons follow.
- Dreamcast games in a CHD or GDI show their disc label (`0GDTEX.PVR`, the picture printed on the disc) in the Discs tab, when the disc has one; the app reads the PVR texture formats used for it (twiddled, VQ-compressed or plain, with or without mipmaps).
- Xbox games show their title image (from `default.xbe`, in DXT or plain texture formats) in the Xbox → XISO tab.

### Changed
- In lists with icons, every icon takes the width of the widest one and rows without an icon keep the same space, so the names line up (for example Wii rows next to GameCube banners, or PSP icons next to Dreamcast labels).
- The README notes that the disc hashes of the RetroAchievements check were confirmed with real Dreamcast, PS1 and PSP games.

## [2.9.0] - 2026-11-16

### Added
- **Check RetroAchievements** in the ROMs tab tells, in the status column, whether each ROM, ZIP or 7z is a version RetroAchievements recognizes (with the game's name and number of achievements), from the hash RetroAchievements identifies games by, computed with its rules per console. The game lists come from the RetroAchievements web API, with the user's own API key, kept in `gameforge.json` (`retroachievements_key`) and set in the **Names database ▾** menu; they are cached in `retroachievements.cache`.
- When RetroAchievements has a game but not the version checked, the status says so ("only another version is supported") and the log links to the supported versions.
- **Check RetroAchievements** in the Discs tab does the same for CHD, ISO, CUE and GDI images of PS1, PS2, PSP, Dreamcast, Saturn and Sega CD discs, with the disc hashes of rcheevos (boot executable, PARAM.SFO and EBOOT.BIN, IP.BIN and boot file, or the disc header), reading only those files.
- **Rename…** in the GameCube/Wii → RVZ tab gives ISO, GCM and RVZ files their Redump names, with the Discs tab's databases. RVZ files are listed in the tab (game ID, title and image size from the RVZ header), not converted.
- GameCube games show their banner (`opening.bnr`) next to their name in the GameCube/Wii → RVZ tab, for ISO, GCM and RVZ files; the RVZ reader can read any part of a GameCube RVZ without decompressing the whole disc.
- The 7z reader can unpack files, not only check them.

### Changed
- The **ROMs → 7z** tab is now called **ROMs**, since it also renames and checks; it and the **Discs** tab are the last tabs.
- The downloaded databases (`titledb.cache`, `redump.cache`, `nointro.cache`) are kept in a `cache` folder next to `gameforge.json`; older ones are moved there at startup.
- `gameforge.json` is written readable by the user only (Linux), since it may hold the API key.
- The note in the `keys` folder is now `keys/README.txt`, in English (it was `LEIA-ME.txt`, in Portuguese).

### Fixed
- A ZIP or 7z with a ROM and a note or image (such as a download site's `.txt`) was taken for an arcade set and not renamed; such files inside an archive no longer count.

## [2.8.0] - 2026-11-15

### Added
- **Rename…** in the ROMs → 7z tab gives ROMs, ZIPs and 7z files their No-Intro names (e.g. `Pokemon - Crystal Version (USA, Europe) (Rev 1).gbc`), found by the ROM's CRC-32 and size, read from the archive's header without unpacking. ROMs with an iNES, fwNES, Lynx, Atari 7800 or SNES copier header, and Nintendo 64 `.v64`/`.n64` files, are found too. Archives with several files, such as arcade sets, keep their names; RetroArch files named after the game follow it.
- The No-Intro databases published by libretro are downloaded on demand per console (29 consoles) and cached in `nointro.cache`, with a **Names database ▾** menu in the tab to update them.
- The ROMs → 7z tab lists 7z files too (to rename them; they are not compressed again), and the 7z reader reads the file list of 7-Zip's archives with a compressed header.

### Fixed
- Since 2.7.0, Tk's file and folder dialogs no longer listed the drives (`C:\`, `E:\`…) in the folder menu on Windows, and a folder selected in the folder dialog could need a second click: opening a dialog reloaded Tk's dialog code and undid the app's changes to it.

## [2.7.1] - 2026-11-15

### Changed
- Renaming disc images in the Discs tab also renames the RetroArch files named after the game in the same folder (save states such as `.state.auto` and `.state1`, `.srm` saves, `.png` thumbnails, per-game cheats and options), so they keep working; the preview counts them.
- The About box describes the ROMs → 7z tab and the Redump names, credits their sources and has a clickable link to the project's GitHub page (it is now a window of its own, of fixed size).

### Fixed
- On Linux, message boxes such as About could be resized.

## [2.7.0] - 2026-11-14

### Added
- PSP games show their icon (`ICON0.PNG`) next to their name in the Discs tab, in its own proportions.
- **Names database ▾** menu in the Discs tab, to update the Redump databases of all consoles, with their state (consoles, data date, a hint after 30 days) next to it.
- Tk's file and folder dialogs (Linux, and Windows when the app's language differs from the system's) hide hidden files and folders, with a **Show hidden files and directories** box whose state is remembered (`show_hidden`).
- Each tab keeps its own output folder (`output_dirs`), which follows the folder opened, or the files added or dropped, in that tab.

### Changed
- The **Discs → CHD** tab is now called **Discs**, since it also shows game information and renames.

### Fixed
- In the clam theme (Linux), the selected tab was narrower than the others, with its text lower.
- On Windows, Tk's dialogs listed names starting with a dot and files with the hidden attribute.

## [2.6.0] - 2026-11-14

### Added
- **Rename…** in the Discs tab gives CHD and ISO files their Redump names (e.g. `Ridge Racer (Europe).chd`), found by the product code on the disc; between entries sharing a code, the regions, the revision (GameCube and Wii), the data track size and, if needed, its CRC decide. `.m3u` playlists that list renamed files are updated.
- The Redump databases published by libretro (CC BY-SA 4.0) are downloaded on demand per console and cached in `redump.cache`.

## [2.5.0] - 2026-11-14

### Added
- The Discs tab shows what each disc says about its game: title, product code, version, regions and disc number (PSP `PARAM.SFO`, Dreamcast `IP.BIN`, Saturn and Sega CD headers, PS1/PS2 `SYSTEM.CNF`, GameCube/Wii header).
- CHD files you already have are listed too (read directly, CD, GD-ROM or DVD, made by this app or by `chdman`), with **Check CHD…** on right-click.

## [2.4.0] - 2026-11-14

### Added
- **ROMs → 7z** tab: compresses cartridge ROMs, or recompresses ZIP files such as arcade sets, to 7z, one archive per file, with LZMA at 7-Zip's normal level. Checkboxes choose the extensions; RetroArch files (`.auto`, `.srm`, `.png`…) start unchecked.
- Optional removal of the originals after verifying (to the Recycle Bin on Windows).

## [2.3.1] - 2026-11-13

### Changed
- The log shows file names without their folders, with a blank line between files.
- Each file's status shows how long its conversion took.

## [2.3.0] - 2026-11-13

### Added
- **Rename…** in the Switch tab gives Switch files their standard names (`Name [TitleID][vN]`), with the official title from the file or the game database.

## [2.2.1] - 2026-11-13

### Changed
- The state of the Switch game database is shown in the Switch tab, next to the **Updates and DLCs** menu (formerly "Online"), instead of in the log at startup.

## [2.2.0] - 2026-11-13

### Added
- RVZ block size option (128 KiB to 2 MiB).

### Changed
- RVZ always uses Zstandard's highest level; decompression speed does not depend on it.

## [2.1.0] - 2026-11-13

### Added
- **About…** button with the version.
- The keys bar tells when `prod.keys` is newer than the firmware table the app knows.

### Changed
- CD audio in CHD is compressed with FLAC linear prediction (LPC), as chdman does: 4 to 5 percent smaller on discs with audio tracks.
- Clearer message when an update cannot be applied to the base game, with the solution.

### Removed
- The RVZ → ISO and CSO/ZSO → ISO conversions.

## [2.0.0]

### Added
- First version as GameForge, with tabs for disc images besides Nintendo Switch: conversion to CHD, Xbox XISO, GameCube/Wii RVZ and CSO/ZSO.

## 1.x

nspmerge: merging Nintendo Switch base games, updates and DLCs, with NSZ compression.

[2.23.5]: https://github.com/tschertel/gameforge/releases/tag/v2.23.5
[2.23.4]: https://github.com/tschertel/gameforge/releases/tag/v2.23.4
[2.23.3]: https://github.com/tschertel/gameforge/releases/tag/v2.23.3
[2.23.2]: https://github.com/tschertel/gameforge/releases/tag/v2.23.2
[2.23.1]: https://github.com/tschertel/gameforge/releases/tag/v2.23.1
[2.23.0]: https://github.com/tschertel/gameforge/releases/tag/v2.23.0
[2.22.0]: https://github.com/tschertel/gameforge/releases/tag/v2.22.0
[2.21.1]: https://github.com/tschertel/gameforge/releases/tag/v2.21.1
[2.21.0]: https://github.com/tschertel/gameforge/releases/tag/v2.21.0
[2.20.3]: https://github.com/tschertel/gameforge/releases/tag/v2.20.3
[2.20.2]: https://github.com/tschertel/gameforge/releases/tag/v2.20.2
[2.20.1]: https://github.com/tschertel/gameforge/releases/tag/v2.20.1
[2.20.0]: https://github.com/tschertel/gameforge/releases/tag/v2.20.0
[2.19.0]: https://github.com/tschertel/gameforge/releases/tag/v2.19.0
[2.18.0]: https://github.com/tschertel/gameforge/releases/tag/v2.18.0
[2.17.1]: https://github.com/tschertel/gameforge/releases/tag/v2.17.1
[2.17.0]: https://github.com/tschertel/gameforge/releases/tag/v2.17.0
[2.16.0]: https://github.com/tschertel/gameforge/releases/tag/v2.16.0
[2.15.0]: https://github.com/tschertel/gameforge/releases/tag/v2.15.0
[2.14.0]: https://github.com/tschertel/gameforge/releases/tag/v2.14.0
[2.13.1]: https://github.com/tschertel/gameforge/releases/tag/v2.13.1
[2.13.0]: https://github.com/tschertel/gameforge/releases/tag/v2.13.0
[2.12.0]: https://github.com/tschertel/gameforge/releases/tag/v2.12.0
[2.11.1]: https://github.com/tschertel/gameforge/releases/tag/v2.11.1
[2.11.0]: https://github.com/tschertel/gameforge/releases/tag/v2.11.0
[2.10.0]: https://github.com/tschertel/gameforge/releases/tag/v2.10.0
[2.9.0]: https://github.com/tschertel/gameforge/releases/tag/v2.9.0
[2.8.0]: https://github.com/tschertel/gameforge/releases/tag/v2.8.0
[2.7.1]: https://github.com/tschertel/gameforge/releases/tag/v2.7.1
[2.7.0]: https://github.com/tschertel/gameforge/releases/tag/v2.7.0
[2.6.0]: https://github.com/tschertel/gameforge/tree/v2.6.0
[2.5.0]: https://github.com/tschertel/gameforge/tree/v2.5.0
[2.4.0]: https://github.com/tschertel/gameforge/tree/v2.4.0
[2.3.1]: https://github.com/tschertel/gameforge/tree/v2.3.1
