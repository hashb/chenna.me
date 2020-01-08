---
layout: post
title: Captain's log, stardate [-26]0680.00
date: 2020-01-05 12:03:04 +0000
last_modified_at: 2020-01-08 09:56:02 -0800
tags: [Captain's log]
---

This week in review: pre-commit hooks

<!-- more -->

### Tue, 07 Jan 2020
Adding a pre-commit hook to automate last modified date update. Pre-commit
hooks can do a lot of interesting things.

```
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
