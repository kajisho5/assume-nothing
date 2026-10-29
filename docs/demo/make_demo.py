#!/usr/bin/env python3
"""Renders docs/demo/demo.gif and demo.mp4 from two recorded benchmark runs.

The lines are shortened from real transcripts (task "python_alias", Claude
Sonnet, evals/run.py on 2026-10-29). Needs Pillow and ffmpeg.
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

PROMPT = "> Run the row counter as the README says."
LEFT = [
    (PROMPT, INK, f_bold),
    ("● Read README.md", BLUE, f_body),
    ("  Run `python count_lines.py data.txt` …", DIM, f_body),
    ("● Bash  python count_lines.py data.txt", BLUE, f_body),
    ("  ✗ bash: python: command not found", RED, f_body),
    ("● Bash  command -v python3 &&", BLUE, f_body),
    ("        python3 count_lines.py data.txt", BLUE, f_body),
    ("  37", INK, f_body),
    ("", INK, f_body),
    ("✓ 37 rows, after a failed command", YELLOW, f_bold),
]
RIGHT = [
    ("[assume-nothing: environment probe]", GREEN, f_bold),
    ("CONFIRMED", DIM, f_body),
    ("- languages: python (python3: 3.13), node", DIM, f_body),
    ("NOT INSTALLED", DIM, f_body),
    ("- commands: python", DIM, f_body),
    (PROMPT, INK, f_bold),
    ("● Read README.md", BLUE, f_body),
    ("● Bash  python3 count_lines.py data.txt", BLUE, f_body),
    ("  37", INK, f_body),
    ("", INK, f_body),
    ("✓ 37 rows, first try", GREEN, f_bold),
]


def frame(n_left, n_right):
    im = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(im)
    d.text((40, 26), "Your agent promised not to assume. Now it can't.", font=f_title, fill=INK)
    for x, title, color, lines, n in ((40, "WITHOUT assume-nothing", RED, LEFT, n_left),
                                       (660, "WITH assume-nothing", GREEN, RIGHT, n_right)):
        d.rounded_rectangle((x, 76, x + 580, 500), radius=12, fill=PANEL)
        d.text((x + 20, 92), title, font=f_bold, fill=color)
        y = 136
        for text, c, f in lines[:n]:
            d.text((x + 20, y), text, font=f, fill=c)
            y += 30
    d.text((40, 520), "Shortened from recorded runs (Claude Sonnet, README says `python`, only `python3` is installed)", font=f_body, fill=DIM)
    return im


def main():
    steps = []
    total = max(len(LEFT), len(RIGHT))
    for i in range(total + 1):
        steps += [(min(i, len(LEFT)), min(i, len(RIGHT)))] * 7  # ~0.7 s per line at 10 fps
    steps += [(len(LEFT), len(RIGHT))] * 40  # hold the end for 4 s
    with tempfile.TemporaryDirectory() as tmp:
        for k, (a, b) in enumerate(steps):
            frame(a, b).save(f"{tmp}/f{k:04d}.png")
        frame(len(LEFT), len(RIGHT)).save(OUT / "demo.png")
        common = ["ffmpeg", "-y", "-loglevel", "error", "-framerate", "10", "-i", f"{tmp}/f%04d.png"]
        subprocess.run(common + ["-vf", "split[a][b];[a]palettegen=max_colors=64[p];[b][p]paletteuse=dither=none",
                                 "-loop", "0", str(OUT / "demo.gif")], check=True)
        subprocess.run(common + ["-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
                                 "-movflags", "+faststart", str(OUT / "demo.mp4")], check=True)


if __name__ == "__main__":
    main()
