# One-shot icon generator: eagles.jpg -> appicon + ico + png icons.
# Run from the repo root: python build/make_icons.py
# No dependencies beyond Pillow.
#
# The white artwork arrived flattened onto a checkerboard "transparency" plate,
# and a white eagle cannot be keyed out of it: its feathers are the plate's own
# white, so every cut-out rule also eats the bird. The same illustration exists
# in blue (eagle_silhouette.jpg), which separates from the plate cleanly — so the
# silhouette is taken from that copy and used to cut the white one. The result
# keeps that alpha: the icon is the eagle alone, on nothing.
from PIL import Image, ImageFilter
from collections import deque
import os

BASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ICONS = os.path.join(BASE, 'frontend', 'public', 'fa', 'icon')
SRC = os.path.join(ICONS, 'eagles.jpg')
SIL = os.path.join(ICONS, 'eagle_silhouette.jpg')
# The green drawing, worn by the tray while the machine is proxied. It is fully
# saturated, so it keys straight out of the plate — no helper copy needed.
ACTIVE = os.path.join(ICONS, 'eagle_active.jpg')


def silhouette(path):
    """Alpha of the drawing, flood keyed from the border over the plate's greys."""
    im = Image.open(path).convert('RGB')
    w, h = im.size
    px = im.load()

    def plate(p):
        r, g, b = p
        return max(r, g, b) - min(r, g, b) <= 10 and min(r, g, b) >= 205

    out = bytearray(w * h)
    q = deque()
    for x in range(w):
        q.append((x, 0))
        q.append((x, h - 1))
    for y in range(h):
        q.append((0, y))
        q.append((w - 1, y))
    while q:
        x, y = q.popleft()
        i = y * w + x
        if out[i] or not plate(px[x, y]):
            continue
        out[i] = 1
        if x > 0:
            q.append((x - 1, y))
        if x + 1 < w:
            q.append((x + 1, y))
        if y > 0:
            q.append((x, y - 1))
        if y + 1 < h:
            q.append((x, y + 1))

    mask = Image.frombytes('L', (w, h), bytes(0 if out[i] else 255 for i in range(w * h)))
    # The keyed edge carries the plate's blur; a median pass takes it off.
    return mask.filter(ImageFilter.MedianFilter(5)).filter(ImageFilter.GaussianBlur(1.0))


# The tray reads PNG frames, which LoadImage understands and which keep the file
# small.
ICO_SIZES = [(s, s) for s in (16, 24, 32, 48, 64, 128, 256)]


def square(art_path, mask):
    """The artwork cut with the silhouette, on nothing, in a square 1024 canvas."""
    bbox = mask.getbbox()
    w, h = mask.size
    art = Image.open(art_path).convert('RGB').resize((w, h), Image.LANCZOS).convert('RGBA')
    art.putalpha(mask)
    art = art.crop(bbox)
    # On nothing: the icon carries its own alpha, so a light surface shows its own
    # colour behind the bird rather than a square of somebody else's background.
    side = max(art.size)
    margin = int(side * 0.02)
    canvas = Image.new('RGBA', (side + margin * 2, side + margin * 2), (0, 0, 0, 0))
    canvas.paste(art, ((canvas.size[0] - art.size[0]) // 2, (canvas.size[1] - art.size[1]) // 2), art)
    return canvas.resize((1024, 1024), Image.LANCZOS)


big = square(SRC, silhouette(SIL))
big.save(os.path.join(BASE, 'build', 'appicon.png'))
big.save(os.path.join(ICONS, 'eagle.ico'), sizes=ICO_SIZES)
big.resize((64, 64), Image.LANCZOS).save(os.path.join(ICONS, 'eagle_64.png'))
big.resize((32, 32), Image.LANCZOS).save(os.path.join(ICONS, 'eagle_32.png'))

# The tray's second face, worn while the machine is actually proxied (system proxy
# or TUN on): the green drawing, keyed on its own alpha. A glance at the tray then
# says whether traffic is going through the node.
square(ACTIVE, silhouette(ACTIVE)).save(os.path.join(ICONS, 'eagle_active.ico'), sizes=ICO_SIZES)

print('done: appicon.png, eagle.ico + eagle_active.ico (PNG frames, for the tray), eagle_64.png, eagle_32.png')
print('run "go run ./tools/mkico frontend/public/fa/icon/eagle.ico build/windows/icon.ico"')
print('or just build, which does it for you (build/release.ps1).')
