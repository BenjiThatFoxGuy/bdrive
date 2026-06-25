> ⚠️ **Unofficial Fork Disclaimer**
> This is an **unofficial fork** of [Teldrive](https://github.com/tgdrive/teldrive), rebranded as BDrive.
> I am **not affiliated with the upstream maintainers**, and this fork **does not intend to be malicious or harmful** in any way.
> Please **read the source code** if you're unsure or want to verify that it behaves as described.
> Contributions, feedback, and scrutiny are welcome.

---

# BDrive
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/benjithatfoxguy/bdrive)

BDrive is a powerful utility that enables you to organise your telegram files and much more.

## Advantages Over Alternative Solutions

- **Exceptional Speed:** BDrive stands out among similar tools, thanks to its implementation in Go, a language known for its efficiency. Its performance surpasses alternatives written in Python and other languages, with the exception of Rust.

- **Enhanced Management Capabilities:** BDrive not only excels in speed but also offers an intuitive user interface for efficient file interaction which other tool lacks. Its compatibility with Rclone further enhances file management.

> [!IMPORTANT]
> BDrive functions as a wrapper over your Telegram account, simplifying file access. However, users must adhere to the limitations imposed by the Telegram API. BDrive is not responsible for any consequences arising from non-compliance with these API limits.You will be banned instantly if you misuse telegram API.

Visit https://bdrive-docs.pages.dev for setting up BDrive.

## Improvements Over Upstream Teldrive

This section tracks functional changes made in this fork relative to [tgdrive/teldrive](https://github.com/tgdrive/teldrive), the project BDrive is forked from. This is distinct from the "Advantages Over Alternative Solutions" section above, which compares BDrive to unrelated third-party tools.

- **Fixed: downloading multiple selected files only downloaded the last one.** Triggering several downloads in a row via `location.href` navigation caused the browser to cancel every in-flight download except the final one, so selecting and downloading multiple files would silently only save the last file. Each file is now downloaded through its own `<a download>` click, staggered to avoid browsers dropping back-to-back downloads. ([bdrive-ui#b9f6a30](https://github.com/BenjiThatFoxGuy/bdrive-ui/commit/b9f6a30ffe2426860d2b7c408b30e2b89cd0c1a1))

- **Added: "Download as Zip" as a multi-download alternative.** Firefox and Safari both silently block every download in a batch past the first one until the user grants the site one-time permission, which is a browser security policy and not fixable from the page. Selecting multiple files now shows a "Download as Zip" action backed by server-side `POST /files/zip` (your own files) and `POST /shares/{id}/zip` (files in a share owned by someone else) endpoints, which stream a zip archive (`archive/zip` over `io.Pipe`, piped straight from the Telegram-storage reader) so the server never buffers the whole archive in memory. This replaces an earlier client-side-only `jszip` implementation. Controlled by a new `files.enable-zip-download` config setting (default on) — both endpoints return 403 when disabled, and the UI hides the action based on the server's `GET /config` `zipDownloadEnabled` flag. ([bdrive#54ccb53](https://github.com/BenjiThatFoxGuy/bdrive/commit/54ccb53c8b382d44b910164071b0a95540df4e34), [bdrive-ui#599cd66](https://github.com/BenjiThatFoxGuy/bdrive-ui/commit/599cd66e15b39c24c471e5f02d3e2e3809828828))

- **Fixed: video player always forced a 16:9 aspect ratio, distorting non-widescreen video.** The player hardcoded `aspectRatio = "16:9"`, which stretched vertical phone clips, 4:3 video, and anything else non-widescreen to fill a 16:9 box (`object-fit: fill`). It now defaults to the source's native aspect ratio (`object-fit: contain`, letterboxing instead of distorting). ([bdrive-ui#e2b31f1](https://github.com/BenjiThatFoxGuy/bdrive-ui/commit/e2b31f18fb295d8dd69cbc360ba996753d1655f7), fixes [tgdrive/teldrive#577](https://github.com/tgdrive/teldrive/issues/577))

- **Added: "Show in Folder" navigates directly to the file's actual folder and highlights it.** When triggered from search or recent results, the file's full folder path is resolved and you're taken straight to that location with the file selected. Has a "Use path navigation for 'Show in folder'" setting (enabled by default) that falls back to a generic browse view when disabled. Hidden from share recipients viewing someone else's shared list for security. ([bdrive#fab018d](https://github.com/BenjiThatFoxGuy/bdrive/commit/fab018d279302b69c577fa8900cee960febb45e3), [bdrive-ui#919b941](https://github.com/BenjiThatFoxGuy/bdrive-ui/commit/919b9415923daed23d42db05e63109926b4712e0), [bdrive-ui#7c20baa](https://github.com/BenjiThatFoxGuy/bdrive-ui/commit/7c20baaa3e127bf35cbbe25ec446e369b29036db))

# Recognitions

<a href="https://trendshift.io/repositories/7568" target="_blank"><img src="https://trendshift.io/api/badge/repositories/7568" alt="divyam234%2Fteldrive | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/></a>

## Best Practices for Using BDrive

### Dos:

- **Follow Limits:** Adhere to the limits imposed by Telegram servers to avoid account bans and automatic deletion of your channel.Your files will be removed from telegram servers if you try to abuse the service as most people have zero brains they will still do so good luck.
- **Responsible Storage:** Be mindful of the content you store on Telegram. Utilize storage efficiently and only keep data that serves a purpose.
  
### Don'ts:
- **Data Hoarding:** Avoid excessive data hoarding, as it not only violates Telegram's terms.
  
By following these guidelines, you contribute to the responsible and effective use of Telegram, maintaining a fair and equitable environment for all users.

## Contributing

Feel free to contribute to this project.See [CONTRIBUTING.md](CONTRIBUTING.md) for more information.

## Donate

If you like this project small contribution would be appreciated [Paypal](https://paypal.me/redux234).

## Star History

<a href="https://www.star-history.com/#benjithatfoxguy/bdrive&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=benjithatfoxguy/bdrive&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=benjithatfoxguy/bdrive&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=benjithatfoxguy/bdrive&type=Date" />
 </picture>
</a>
