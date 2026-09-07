"""Rebuild the local PDF font from the pinned official Noto source (dev only)."""

import gzip
import hashlib
import io
from pathlib import Path
import sys

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: python rebuild.py /path/to/NotoSansSC-VF.ttf")
    source = Path(sys.argv[1]).read_bytes()
    expected = "d68bafcb48a2707749396aa12bbbd833cb70401f3a9a689fd2902c7e0d295964"
    if hashlib.sha256(source).hexdigest() != expected:
        raise SystemExit("source font SHA-256 differs from the reviewed release")
    font = TTFont(io.BytesIO(source), recalcTimestamp=False)
    instantiateVariableFont(font, {"wght": 400}, inplace=True)
    options = subset.Options()
    options.layout_features = []
    options.name_IDs = ["*"]
    selector = subset.Subsetter(options=options)
    selector.populate(unicodes=[r for r in font.getBestCmap() if r <= 0xFFFF])
    selector.subset(font)
    names = {
        1: "Lunitide Sans SC", 2: "Regular", 3: "LunitideSansSC-Regular-2.004",
        4: "Lunitide Sans SC Regular", 6: "LunitideSansSC-Regular",
        16: "Lunitide Sans SC", 17: "Regular",
    }
    for entry in font["name"].names:
        if entry.nameID in names:
            entry.string = names[entry.nameID].encode(entry.getEncoding())
    buffer = io.BytesIO()
    font.save(buffer)
    raw = buffer.getvalue()
    compressed = gzip.compress(raw, mtime=0)
    coverage = bytearray(8192)
    for rune, glyph in font.getBestCmap().items():
        if rune <= 0xFFFF and font.getGlyphID(glyph):
            coverage[rune // 8] |= 1 << (rune % 8)
    destination = Path(__file__).resolve().parent
    (destination / "LunitideSansSC-Regular.ttf.gz").write_bytes(compressed)
    (destination / "LunitideSansSC-Regular.cmap").write_bytes(coverage)
    for label, data in [("TTF", raw), ("gzip", compressed), ("coverage", coverage)]:
        print(label, len(data), hashlib.sha256(data).hexdigest())


if __name__ == "__main__":
    main()
