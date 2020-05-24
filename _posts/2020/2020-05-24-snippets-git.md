---
layout: post
title: 'Snippets: Git'
date: 2020-05-24 16:30 +0000
last_modified_at: 2020-05-24 16:53:06 +0000
tags: [Productivity, Tools]
published: true
---

## Working with remote
When you have a fork of a repository, you might want to sync it with one another.
This can be done using git remotes

```bash
# list all remotes
git remote -v

# change remote url
git remote set-url <name of remote> <new url>

# Add new remote url
git remote add <name of remote> <url>

# rename remote
git remote rename <old name> <new name>

```
