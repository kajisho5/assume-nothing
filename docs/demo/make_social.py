#!/usr/bin/env python3
"""Renders the social preview (GitHub, 1280x640) in English and Japanese.

Numbers come from the README benchmark (v0.14.0, 10 runs per task and
condition, 2026-10-30). Needs Pillow and the Inter and Noto Sans CJK fonts.
"""
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

OUT = Path(__file__).resolve().parent
W, H = 1280, 640
BG, CARD, LINE = (13, 17, 23), (22, 27, 34), (48, 54, 61)
INK, DIM, GREEN = (230, 237, 243), (139, 148, 158), (63, 185, 80)
INTER = "/usr/share/fonts/opentype/inter/Inter-{}.otf"
CJK = "/usr/share/fonts/opentype/noto/NotoSansCJK-{}.ttc"
MONO = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"

TEXT = {
    "en": {
        "tag": "Your agent promised not to assume. Now it can't.",
        "sub": "Hooks for Claude Code · Cursor · Codex · Gemini CLI · OpenCode",
        "cards": [
            ("0", "missing commands run", "4 models; 0.7–0.8 per task without it"),
            ("16 → 30", "right first time, out of 30", "Sonnet, facts only in the project docs"),
            ("−68%", "tokens to a right answer", "Sonnet, re-asks included"),
        ],
    },
    "ja": {
        "tag": "AIは「思い込まない」と約束した。これでもう、思い込めない。",
        "sub": "Claude Code・Cursor・Codex・Gemini CLI・OpenCode 用のフック",
        "cards": [
            ("0", "入っていないコマンドを打った回数", "4種類のAIで。なしだと1作業0.7〜0.8回"),
            ("16 → 30", "1回目で正解（30問中）", "Sonnet、資料にしか無い答え"),
            ("−68%", "正解までのトークン", "Sonnet、聞き直し込み"),
        ],
    },
}


def font(lang, weight, size):
    if lang == "ja":
        return ImageFont.truetype(CJK.format({"ExtraBold": "Black", "SemiBold": "Bold"}.get(weight, weight)), size, index=0)
    return ImageFont.truetype(INTER.format(weight), size)


def fit(d, text, f_make, size, width):
    while size > 10:
        f = f_make(size)
        if d.textlength(text, font=f) <= width:
            return f
        size -= 1
    return f_make(size)


def render(lang):
    t = TEXT[lang]
    im = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(im)
    icon = Image.open(OUT / "icon-256.png").convert("RGBA").resize((132, 132), Image.LANCZOS)
    im.paste(icon, (72, 64), icon)
    d.text((232, 70), "assume-nothing", font=ImageFont.truetype(INTER.format("ExtraBold"), 78), fill=INK)
    d.text((236, 168), "kajisho5/assume-nothing", font=ImageFont.truetype(MONO, 24), fill=DIM)
    f_tag = fit(d, t["tag"], lambda s: font(lang, "SemiBold", s), 40, W - 144)
    d.text((72, 236), t["tag"], font=f_tag, fill=INK)
    f_sub = fit(d, t["sub"], lambda s: font(lang, "Regular", s), 26, W - 144)
    d.text((72, 300), t["sub"], font=f_sub, fill=DIM)

    gap, top, bottom = 24, 370, 568
    cw = (W - 144 - 2 * gap) // 3
    for i, (big, label, note) in enumerate(t["cards"]):
        x = 72 + i * (cw + gap)
        d.rounded_rectangle((x, top, x + cw, bottom), radius=16, fill=CARD, outline=LINE, width=2)
        d.text((x + 28, top + 20), big, font=fit(d, big, lambda s: ImageFont.truetype(INTER.format("ExtraBold"), s), 72, cw - 56), fill=GREEN)
        d.text((x + 28, top + 116), label, font=fit(d, label, lambda s: font(lang, "SemiBold", s), 24, cw - 56), fill=INK)
        d.text((x + 28, top + 152), note, font=fit(d, note, lambda s: font(lang, "Regular", s), 19, cw - 56), fill=DIM)
    small = "Benchmark v0.14.0, 10 runs per task" if lang == "en" else "比較試験 v0.14.0、各10回"
    d.text((72, 592), small, font=font(lang, "Regular", 18), fill=DIM)
    name = "social-preview.png" if lang == "en" else "social-preview-ja.png"
    im.save(OUT / name, optimize=True)
    return OUT / name


if __name__ == "__main__":
    for lang in ("en", "ja"):
        print(render(lang))
