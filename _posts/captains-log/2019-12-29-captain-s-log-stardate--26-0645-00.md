---
layout: post
title: Captain's log, stardate [-26]0645.00
date: 2019-12-29 12:03:10 +0000
last_modified_at: 2019-12-31 14:01:28 -0800
tags: [Captain's log]
---

This week in review: decade is over.

<!-- more -->

### Tue, 31 Dec 2019
End of a Decade!

TODO: figure out what I did for the past 10 years.

### Thu, 02 Jan 2020
I use tmux in my workflow and recently have had to use VS Code as part of my
dev environment. VS Code injects couple of environment variables into the shell
to setup wsl server that talks to the windows native code editor. Since, I was
launching tmux at startup, I ran into the issue of tmux missing env variables
required for VS Code and its wsl server. Here is a hack I am currently using
to get around this issue. 

```bash
if [[ -z "$TMUX" ]] ;then
    ID="$( tmux ls | grep -vm1 attached | cut -d: -f1 )" # get the id of a deattached session
    if [[ "$TERM_PROGRAM" == "vscode" ]]; then
        tmux new-session -s vscode-$(date +%s)
    elif [[ -z "$ID" ]] ;then # if not available create a new one
        tmux new-session
    else
        tmux attach-session -t "$ID" # if available attach to it
    fi
fi
```
