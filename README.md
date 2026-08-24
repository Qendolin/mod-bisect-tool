# Mod Bisect Tool

A powerful, easy-to-use tool to find conflicting **Fabric**, **Quilt** and **(Neo)Forge** mods.

If your game is having issues, like crashing on startup or exhibiting strange bugs, and you have hundreds of mods, finding the culprit can be a nightmare. This tool automates that process by performing a guided search, quickly pinpointing exactly which mod or combination of mods is causing the failure.

> [!WARNING]
> ## When Should You Use This Tool?
> This tool is powerful, but it can be time-consuming. **Always check your crash report or game log first!** Often, the log will directly name the problematic mod.
>
> **Use this tool when:**
> * The crash report does not name a specific mod.
> * The game doesn't crash but has a subtle in-game bug.
> * You suspect a conflict between two or more mods that the logs don't show.

## How It Works

> [!IMPORTANT]
> Please read the following instructions carefully to effectively use the Mod Bisect Tool. Understanding these steps will help you quickly resolve your mod conflicts.

This tool is like a smart detective for your mods folder. Instead of you having to manually guess which mods to disable, it does the hard work for you through a methodical process.

1.  **Fast Search:** It performs a "bisection search" to quickly narrow down the list of potential troublemakers. It will ask you to launch the game with different sets of mods enabled, using your feedback to eliminate half of the remaining candidates in each step.
2.  **Pinpoints the Problem:** After a few steps, it will tell you exactly which mod or group of mods is causing the issue.
3.  **Finds Multiple Conflicts:** If you have more than one mod conflict, the tool is smart enough to find the first one, let you set it aside, and then **continue searching** the rest of your mods to find other, unrelated conflicts.

## Installation

Use the [Mod Bisect Tool download page](https://Qendolin.github.io/mod-bisect-tool/#downloads).
It detects your operating system, lets you choose the graphical or terminal version,
and recommends the appropriate architecture. The selected filename and any required
launch instructions are shown alongside the download.


## User Guides

The tool comes in two flavors, each with its own guide:

* **[GUI User Guide](https://Qendolin.github.io/mod-bisect-tool/GUI-User-Guide.html)** - a graphical application for Windows, Linux and macOS.
* **[TUI User Guide](https://Qendolin.github.io/mod-bisect-tool/TUI-User-Guide.html)** - a terminal application with keyboard-driven controls.

## Capabilities: What This Tool Can Handle

This tool is built on a powerful dependency resolver and bisection engine, allowing it to handle even the most complex mod-related issues.

* **Automatic Dependency Activation:** You don't need to worry about enabling libraries or APIs yourself. When the tool tests a mod, it automatically enables all of its required dependencies. If `Mod A` needs `Fabric API`, the tool ensures both are active for the test.

* **Complex Conflict Scenarios:**
    * **Single-Mod Conflicts:** The simplest case where one mod causes an issue.
    * **Multi-Mod Conflicts:** The tool can find conflicts that only happen when two or more specific mods (`Mod A` + `Mod B`) are enabled at the same time.
    * **Dependency Conflicts:** It can correctly identify when a conflict is caused by a library (`API X`) that is automatically pulled in as a dependency by another mod.
    * **Nested JARs:** It correctly handles mods that bundle their libraries inside their own JAR file. If a bundled library is the problem, the tool will correctly point to the top-level mod as the cause.

* **Multiple, Unrelated Conflicts:** The "Continue Search" feature allows the tool to find all independent conflicts in your modpack. After finding the first conflict set, you can tell the tool to continue searching through the remaining mods to find other, completely separate problems.

* **Graceful Error Handling:** If you accidentally delete a mod file while a search is in progress, the tool will detect it, notify you, and allow you to continue the search without the missing mod, preventing crashes and preserving your progress.
