---
layout: post
title: Captain's log, stardate [-26]0680.00
date: 2020-01-05 12:03:04 +0000
last_modified_at: 2020-01-09 16:21:36 -0800
tags: [Captain's log]
---

This week in review: pre-commit hooks

<!-- more -->

### Tue, 07 Jan 2020
Adding a pre-commit hook to automate last modified date update. Pre-commit
hooks can do a lot of interesting things.

```bash
#!/bin/sh

# https://blog.nerde.pw/2016/08/09/jekyll-last-modified-date.html
echo ************************************
echo *   updating last_modified_at...   *
echo ************************************
git diff --cached --name-status | while read a b; do
  echo * Processing $b...
  sed -i "/---.*/,/---.*/s/^last_modified_at:.*$/last_modified_at: $(date "+%Y-%m-%d %T %z")/" $b
  git add $b
done
```
▣

### Wed, 08 Jan 2020
zero day!

### Thu, 09 Jan 2020
Automated planning in a nutshell[^1]. I have been looking into high level
Task Planning for robotic systems. The most popular one seems to be the Planning
Domain Description language (PPDL). I think I will write more about this later.

TODO: remember to do a writeup on Camera Calibration.

[^1]: <https://github.com/pellierd/pddl4j/wiki/Automated-planning-in-a-nutshell>
