#!/usr/bin/env python3
"""生成 FNOS VM Remote 的应用图标（圆角方形 + 电源符号 + Windows 窗口丝线）。
输出：ICON.PNG (1024), ICON_256.PNG (256), web icon_64/icon_256。
"""
from PIL import Image, ImageDraw

def rounded(draw, box, radius, fill):
    draw.rounded_rectangle(box, radius=radius, fill=fill)

def make(size):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    s = size / 1024.0
    # 渐变底：深蓝 → 靛蓝（手动分段模拟）
    top, bottom = (37, 99, 235), (30, 64, 175)
    steps = size
    for i in range(steps):
        t = i / (steps - 1)
        c = tuple(int(top[j] + (bottom[j] - top[j]) * t) for j in range(3)) + (255,)
        d.line([(0, i), (size, i)], fill=c)
    # 圆角裁剪
    mask = Image.new("L", (size, size), 0)
    md = ImageDraw.Draw(mask)
    md.rounded_rectangle([0, 0, size - 1, size - 1], radius=int(220 * s), fill=255)
    img.putalpha(mask)

    d = ImageDraw.Draw(img)
    white = (255, 255, 255, 255)
    # 电源符号：圆弧 + 竖线
    cx, cy, r = size / 2, size / 2 + int(-30 * s), int(300 * s)
    width = int(110 * s)
    bbox = [cx - r, cy - r, cx + r, cy + r]
    # 圆弧从 -35° 到 215°（缺口朝上）
    d.arc(bbox, start=-35, end=215, fill=white, width=width)
    # 竖线
    lw = int(105 * s)
    top_y = cy - r - int(10 * s)
    bot_y = cy - int(60 * s)
    d.rounded_rectangle([cx - lw // 2, top_y, cx + lw // 2, bot_y], radius=lw // 2, fill=white)
    # Windows 风格小窗标记：右下角四格
    gx, gy = int(650 * s), int(640 * s)
    cell, gap = int(88 * s), int(16 * s)
    for dy in range(2):
        for dx in range(2):
            x0 = gx + dx * (cell + gap)
            y0 = gy + dy * (cell + gap)
            d.rounded_rectangle([x0, y0, x0 + cell, y0 + cell], radius=int(10 * s), fill=white)
    return img

base = make(1024)
base.resize((1024, 1024), Image.LANCZOS).save("fnos-app/ICON.PNG")
base.resize((256, 256), Image.LANCZOS).save("fnos-app/ICON_256.PNG")
base.resize((256, 256), Image.LANCZOS).save("fnos-app/app/ui/images/icon_256.png")
base.resize((64, 64), Image.LANCZOS).save("fnos-app/app/ui/images/icon_64.png")
base.resize((64, 64), Image.LANCZOS).save("backend/internal/httpx/web/images/icon_64.png")
print("icons done")
