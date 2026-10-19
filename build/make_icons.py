# One-shot icon generator: eagles.jpg -> appicon + ico + png icons.
# Run from the repo root: python build/make_icons.py
# No dependencies beyond Pillow.
from PIL import Image, ImageDraw
import os

BASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(BASE, 'frontend', 'public', 'fa', 'icon', 'eagles.jpg')

im = Image.open(SRC).convert('RGB')
w, h = im.size

# The artwork is a rounded tile on a near-black card. Find its bounding box by
# brightness so the black padding is cropped away before any scaling.
gray = im.convert('L')
bbox = gray.point(lambda p: 255 if p > 40 else 0).getbbox()
print('detected bbox:', bbox, 'of', im.size)
if bbox and (bbox[2] - bbox[0]) > w * 0.5 and (bbox[3] - bbox[1]) > h * 0.5:
    sq = im.crop(bbox)
else:
    side = min(w, h)
    left, top = (w - side) // 2, (h - side) // 2
    sq = im.crop((left, top, left + side, top + side))
    print('fallback center square')

# Square canvas, artwork scaled to fill completely.
side = min(sq.size)
sq = sq.crop(((sq.size[0] - side) // 2, (sq.size[1] - side) // 2,
              (sq.size[0] + side) // 2, (sq.size[1] + side) // 2))
big = sq.resize((1024, 1024), Image.LANCZOS).convert('RGBA')

# Rounded-corner alpha mask (squircle ratio, ~22%) so the corners read as a tile
# rather than a black square on dark surfaces (tray, taskbar).
radius = int(1024 * 0.2237)
mask = Image.new('L', (1024, 1024), 0)
ImageDraw.Draw(mask).rounded_rectangle([0, 0, 1023, 1023], radius=radius, fill=255)
big.putalpha(mask)

big.save(os.path.join(BASE, 'build', 'appicon.png'))

# The icon file the tray reads: PNG frames, which LoadImage understands and
# which keep the file small.
ico_sizes = [(s, s) for s in (16, 24, 32, 48, 64, 128, 256)]
big.save(os.path.join(BASE, 'frontend', 'public', 'fa', 'icon', 'eagle.ico'), sizes=ico_sizes)
big.resize((64, 64), Image.LANCZOS).save(os.path.join(BASE, 'frontend', 'public', 'fa', 'icon', 'eagle_64.png'))
big.resize((32, 32), Image.LANCZOS).save(os.path.join(BASE, 'frontend', 'public', 'fa', 'icon', 'eagle_32.png'))

# The icon file the executable carries: the same picture, but with every frame
# uncompressed, because the window framework asks for it with LoadIcon, which
# reads only that form. The build does this step itself (tools/mkico), so it
# does not have to be done by hand.
print('done: appicon.png, eagle.ico (PNG frames, for the tray), eagle_64.png, eagle_32.png')
print('run "go run ./tools/mkico frontend/public/fa/icon/eagle.ico build/windows/icon.ico"')
print('or just build, which does it for you (build/release.ps1).')
