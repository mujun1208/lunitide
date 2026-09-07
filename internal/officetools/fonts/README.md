# Embedded PDF font

`LunitideSansSC-Regular.ttf.gz` is a modified, regular-weight subset of **Noto Sans SC**, bundled so Chinese PDFs do not depend on operating-system fonts or a network download. It remains licensed under the **SIL Open Font License 1.1**; see [OFL.txt](OFL.txt).

Original copyright metadata retained in the font:

> © 2014-2021 Adobe (http://www.adobe.com/), with Reserved Font Name 'Source'.

Upstream official release: [Noto CJK Sans2.004](https://github.com/notofonts/noto-cjk/tree/Sans2.004). Source font: [NotoSansSC-VF.ttf](https://github.com/notofonts/noto-cjk/blob/Sans2.004/Sans/Variable/TTF/Subset/NotoSansSC-VF.ttf). License: [upstream OFL](https://github.com/notofonts/noto-cjk/blob/Sans2.004/LICENSE). The OFL permits bundling and embedding with software subject to its conditions; this modified font is renamed and is not sold separately. Preserve this notice and OFL.txt with source/asset redistributions. Copyright and license metadata are also retained inside the embedded font.

The release staging script copies the full license to `licenses/NotoSansSC-OFL.txt` and the standalone [NOTICE.txt](NOTICE.txt) to `licenses/NotoSansSC-NOTICE.txt`. Both are required by `Verify-Layout.ps1`, included by the existing installer stage recursion, and covered by the release SHA-256 manifest. They are readable in the installed application's `licenses` directory; no runtime font extraction is required. This round changes the distribution inventory only and does not build or install a package.

## Changes and footprint

- Fixed variable `wght` to 400 (Regular), removed other weights.
- Kept every supported Basic Multilingual Plane character (U+0000–U+FFFF), 30,445 mapped glyphs; removed unsupported supplementary-plane mappings and layout features that the current PDF renderer does not use.
- Renamed family/PostScript identifiers to `Lunitide Sans SC` / `LunitideSansSC-Regular`; preserved original copyright and license metadata.
- Compressed static TTF with deterministic gzip. Product code uses Go's standard gzip reader once and the existing gofpdf subset writer. No new runtime library, paid service or user-installed font is needed.
- Compressed embedded font: **6,125,527 bytes (5.84 MiB)**. Decompressed TTF: **10,206,376 bytes (9.73 MiB)**. Glyph coverage bitmap: **8,192 bytes**. A two-page Chinese verification PDF with 28 long paragraphs was **25,861 bytes**, because generated PDFs contain only used glyphs.
- Unsupported glyphs, including emoji and supplementary CJK characters outside the renderer's supported range, return a clear error before producing a file. They are not silently replaced by empty squares.

SHA-256:

| Asset | SHA-256 |
| --- | --- |
| Upstream NotoSansSC-VF.ttf (17,773,132 bytes) | `d68bafcb48a2707749396aa12bbbd833cb70401f3a9a689fd2902c7e0d295964` |
| Static TTF before gzip | `82b7b44a0e060caaa7bfc21cf10936ddf7452aef44c0f1ecda969403ac6d8fb8` |
| Embedded gzip | `7a6e6064bfff95a48ede8be3b090a1402a03bf72db353706888a52437ab22db2` |

## Rebuild

The product does not run this script. For maintainers, use Python 3.11 with `fonttools==4.64.0` in an isolated development environment, download the pinned official source font above, and run `python rebuild.py /path/to/NotoSansSC-VF.ttf`. The script verifies the source hash before reading it and writes only the two derived font assets next to itself. If intentionally updating the source/toolchain, review the license and glyph coverage, update `pdfFontSHA256`, and rerun PDF roundtrip, line-wrap, missing-glyph and independent rendering checks.
