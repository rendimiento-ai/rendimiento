#!/usr/bin/env python3
"""Draws rendimiento.ico, the icon next to the clock, from rendimiento's
logo (web/index.html). Run once, with Pillow, and commit the result:

    python3 icono.py
"""
from PIL import Image, ImageDraw

N = 32  # the logo's own grid
K = 16  # drawn this many times larger, then reduced: smooth edges


def logo(lado):
    g = N * K
    im = Image.new("RGBA", (g, g), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    d.rounded_rectangle([0, 0, g - 1, g - 1], radius=9 * K, fill="#8E1B2C")
    puntos = [(4, 17), (10, 17), (13, 9), (18, 24), (21, 17), (28, 17)]
    puntos = [(x * K, y * K) for x, y in puntos]
    # Thicker than the logo's 3 at the small sizes, where it would vanish.
    ancho = (3 if lado >= 32 else 4) * K
    d.line(puntos, fill="#F2C14E", width=ancho, joint="curve")
    for x, y in puntos:  # round caps and joins
        d.ellipse([x - ancho / 2, y - ancho / 2, x + ancho / 2, y + ancho / 2], fill="#F2C14E")
    return im.resize((lado, lado), Image.LANCZOS)


lados = [16, 20, 24, 32, 40, 48, 64]
imagenes = [logo(n) for n in lados]
# Plain bitmaps, not PNG: every Windows loader reads them.
imagenes[-1].save("rendimiento.ico", sizes=[(n, n) for n in lados], append_images=imagenes[:-1], bitmap_format="bmp")
