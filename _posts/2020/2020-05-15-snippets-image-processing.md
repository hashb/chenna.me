---
layout: post
title: 'Snippets: Image processing'
date: 2020-05-15 21:06 +0000
last_modified_at: 2020-05-16 01:40:09 +0000
tags: [Productivity, Tools]
---

Useful snippets reference while working with Images

<!-- more -->

I recently had to convert a binary image into a csv file. Here is a quick and
simple way of doing this.

```python
import numpy as np
from PIL import Image

im = np.array(Image.open('fromfile.bmp'), dtype=int)
np.savetxt('tofile.csv', im, fmt='%d', delimiter=',')
```
