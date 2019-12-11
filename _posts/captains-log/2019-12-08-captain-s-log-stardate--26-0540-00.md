---
layout: post
title: Captain's log, stardate [-26]0540.00
date: 2019-12-08 12:02:43 +0000
last_modified_at: 2019-12-08 12:02:43 +0000
tags: [Captain's log]
---

This week in review: Dynamically loading `*.so`, resolving local domains with
mDNS

<!-- more -->

### Tue, 10 Dec 2019

When dynamically loading plug-in objects, create a dependency graph, find
the lowest common ancestor and start loading the objects from there. This
prevents objects failing to load due to unsatisfied dependencies.▣

If you are not able to resolve local hosts with `hostname.local`, you can try
removing `[NOTFOUND=continue]` from `/etc/nsswitch.conf`. This may or may not
help. This asks the mDNS resolver to quit if it cannot resolve.  
TODO: Lookup how Avahi daemon works.▣
