---
layout: post
title: How to read video devices efficiently on Linux
date: 2021-10-30 18:54 +0000
last_modified_at: 2021-10-30 20:06:52 +0000
tags: [v4l2, VideoCapture, opencv, linux, How To]
published: true
---

Using OpenCV VideoCapture on linux uses very high cpu to fetch frames from 
video4linux2 devices and offers low frame rates. 

use imageio-ffmpeg instead