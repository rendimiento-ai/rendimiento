#!/usr/bin/env python3
"""Draws rendimiento.ico, the icon next to the clock, from rendimiento's
mark: the sun over the furrows (web/public/icono.svg, and the Logo in
web/src/components/marca.tsx). Run it with Pillow, from this folder, and
commit the result:

    python3 icono.py
"""
import math

from PIL import Image, ImageDraw

N = 32  # the icon's grid: web/public/icono.svg is 512 = 32 × 16
K = 16  # drawn this many times larger, then reduced: smooth edges
FONDO = "#8E1B2C"  # grana, the accent
TRAZO = "#F2C14E"  # maíz


def _arco(cx, cy, r, de, a, n=48):
    return [(cx + r * math.cos(de + (a - de) * i / n), cy + r * math.sin(de + (a - de) * i / n)) for i in range(n + 1)]


def _curva(p0, c, p1, n=48):
    return [((1 - u) ** 2 * p0[0] + 2 * (1 - u) * u * c[0] + u * u * p1[0],
             (1 - u) ** 2 * p0[1] + 2 * (1 - u) * u * c[1] + u * u * p1[1]) for u in (i / n for i in range(n + 1))]


# The mark's strokes, in its own 24 × 24 grid (the path in marca.tsx), as points to join.
TRAZOS = [
    _arco(12, 13, 6, math.pi, 2 * math.pi),  # M6 13a6 6 0 0 1 12 0: the sun, rising
    [(12, 3), (12, 5)],  # its rays
    [(5, 6), (6.5, 7.5)],
    [(19, 6), (17.5, 7.5)],
    [(2.5, 12.5), (4.5, 12.5)],
    [(19.5, 12.5), (21.5, 12.5)],
    _curva((3, 16.5), (12, 13.5), (21, 16.5)),  # the two furrows
    _curva((3, 20), (12, 17), (21, 20)),
]


def icono(lado):
    g = N * K
    im = Image.new("RGBA", (g, g), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    d.rounded_rectangle([0, 0, g - 1, g - 1], radius=9 * K, fill=FONDO)
    # The svg's stroke is 2.2; thicker at the small sizes, where it would vanish.
    ancho = (2.2 if lado >= 48 else 2.6 if lado >= 32 else 3.0) * K
    r = ancho / 2
    for trazo in TRAZOS:
        # Placed as in icono.svg: the 24 grid, 4 in from the corner.
        pts = [((x + 4) * K, (y + 4) * K) for x, y in trazo]
        d.line(pts, fill=TRAZO, width=round(ancho))
        for x, y in pts:  # round caps and joins
            d.ellipse([x - r, y - r, x + r, y + r], fill=TRAZO)
    return im.resize((lado, lado), Image.LANCZOS)


lados = [16, 20, 24, 32, 40, 48, 64, 128, 256]
imagenes = [icono(n) for n in lados]
# Plain bitmaps, not PNG: every Windows loader reads them.
imagenes[-1].save("rendimiento.ico", sizes=[(n, n) for n in lados], append_images=imagenes[:-1], bitmap_format="bmp")
