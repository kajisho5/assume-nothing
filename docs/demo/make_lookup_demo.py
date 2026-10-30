#!/usr/bin/env python3
"""Renders docs/demo/demo-lookup.gif from a recorded run of the lookup check.

The lines are shortened from a real transcript (Claude Haiku, Claude Code,
2026-10-29, v0.5): asked for the latest stable Bun, the agent answered from
memory, was sent back once, looked it up, and corrected itself. The prompt
was in Japanese; it is shown in English here. Needs Pillow and ffmpeg.
"""
import subprocess
import tempfile
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

OUT = Path(__file__).resolve().parent
W, H = 1280, 560
BG, PANEL, INK, DIM = (13, 17, 23), (22, 27, 34), (230, 237, 243), (139, 148, 158)
RED, GREEN, BLUE, YELLOW = (248, 81, 73), (63, 185, 80), (88, 166, 255), (210, 153, 34)
MONO = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
f_body, f_bold, f_title = ImageFont.truetype(MONO, 17), ImageFont.truetype(BOLD, 17), ImageFont.truetype(BOLD, 22)

LINES = [
    ("> What is the latest stable version of Bun? One line.", INK, f_bold),
    ("[assume-nothing] This request is about: Bun. It asks about something", DIM, f_body),
    ("that changes over time: look it up in this session.", DIM, f_body),
    ("", INK, f_body),
    ("Bun's latest stable version is 1.3.4.        (from memory, no lookup)", YELLOW, f_body),
    ("", INK, f_body),
    ("assume-nothing: sent the answer back once, because nothing looked up Bun.", RED, f_bold),
    ("", INK, f_body),
    ("● WebSearch  Bun latest stable release", BLUE, f_body),
    ("", INK, f_body),
    ("Bun's latest stable version is 1.4.4. The earlier 1.3.4 was wrong.", GREEN, f_bold),
]


def frame(n):
    im = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(im)
    d.text((40, 26), "Answered from memory? Sent back once.", font=f_title, fill=INK)
    d.rounded_rectangle((40, 76, W - 40, 500), radius=12, fill=PANEL)
    y = 100
    for text, c, f in LINES[:n]:
        d.text((64, y), text, font=f, fill=c)
        y += 34
    d.text((40, 520), "Shortened from a recorded run (Claude Haiku, 2026-10-29). The prompt was in Japanese.", font=f_body, fill=DIM)
    return im


def main():
    steps = []
    for i in range(len(LINES) + 1):
        steps += [i] * 8
    steps += [len(LINES)] * 45
    with tempfile.TemporaryDirectory() as tmp:
        for k, n in enumerate(steps):
            frame(n).save(f"{tmp}/f{k:04d}.png")
        frame(len(LINES)).save(OUT / "demo-lookup.png")
        subprocess.run(["ffmpeg", "-y", "-loglevel", "error", "-framerate", "10", "-i", f"{tmp}/f%04d.png",
                        "-vf", "split[a][b];[a]palettegen=max_colors=64[p];[b][p]paletteuse=dither=none",
                        "-loop", "0", str(OUT / "demo-lookup.gif")], check=True)


if __name__ == "__main__":
    main()
