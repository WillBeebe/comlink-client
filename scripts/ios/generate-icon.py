#!/usr/bin/env python3
"""Reproduce the Comlink app icon (Pillow; opaque PNG required by Apple)."""
import json
from pathlib import Path
from PIL import Image, ImageDraw
root = Path(__file__).resolve().parents[2] / 'ios/Comlink/Assets.xcassets'
icons = root / 'AppIcon.appiconset'
icons.mkdir(parents=True, exist_ok=True)
image = Image.new('RGB', (1024, 1024), '#122536')
d = ImageDraw.Draw(image)
# Two interlocking speech bubbles, preserving a clear silhouette at small sizes.
d.rounded_rectangle((180, 225, 710, 615), radius=110, fill='#62E3BD')
d.polygon([(255, 550), (255, 730), (440, 580)], fill='#62E3BD')
d.rounded_rectangle((400, 430, 845, 745), radius=100, fill='#F4F7FC')
d.polygon([(770, 680), (770, 835), (610, 715)], fill='#F4F7FC')
for x in (505, 625, 745): d.ellipse((x-23, 560, x+23, 606), fill='#122536')
image.save(icons / 'AppIcon.png')
(icons / 'Contents.json').write_text(json.dumps({'images':[{'filename':'AppIcon.png','idiom':'universal','platform':'ios','size':'1024x1024'}],'info':{'author':'xcode','version':1}}, indent=2)+'\n')
(root / 'Contents.json').write_text('{"info":{"author":"xcode","version":1}}\n')
