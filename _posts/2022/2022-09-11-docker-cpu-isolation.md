---
layout: post
title: Docker CPU Isolation
date: 2022-09-11 02:26 +0000
last_modified_at: 
tags: [docker, linux]
published: true
---

I recently ran into an issue where I needed to run a docker container with realtime
priority but couldn't use isolcpus or PREEMPT-RT patch. You can use cgroups to isolate
a docker container to specific cores and prevent other processes from using that particular code.
This is different from cpu pinning where other processes can still use the core that the
process is pinned to. 


```bash
#!/usr/bin/env bash

# install cset
pip install git+https://github.com/lpechacek/cpuset.git future

# delete existing cgroup
cset set -d docker

# create a new cgroup with two cores
cset shield --userset=ur_executor --cpu 0,1  -k on

# tell docker to use system cgroup
/bin/cat <<EOF > /etc/docker/daemon.json
{
    "cgroup-parent": "system"
}
EOF

service docker restart

echo "SETUP SUCCESSFUL"
```

while running the docker container, add `--cgroup-parent=ur_executor` to isolate container
to `ur_executor` cgroup
