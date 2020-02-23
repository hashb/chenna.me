---
layout: post
title: How to setup PREEMPT RT on Ubuntu 18.04
date: 2020-02-23 06:11 +0000
last_modified_at: 2020-02-23 06:48:20 +0000
tags: [How To, Realtime, Robotics]
published: true
---

These are my notes on how to install install preempt_rt patch for linux kernel
on Ubuntu 18.04. I am doing this for a robot control application where
non-determinism can cause damage to life or property. I will be writing more
blog posts about real time implementation for robotic applications.

We will be installing RT-PREEMPT kernel on ubuntu 18.04. Kernel version I am
choosing is 5.4 since it is the latest LTS release.

## Install Dependencies

Install compilers required for building the kernel

    sudo apt install build-essential git libssl-dev libelf-dev

## Download and patch

Download the `linux-5.4.17` kernel from [kernel.org](http://kernel.org) and the
rt patch 

    wget https://mirrors.edge.kernel.org/pub/linux/kernel/v5.x/linux-5.4.19.tar.xz
    wget https://mirrors.edge.kernel.org/pub/linux/kernel/projects/rt/5.4/patch-5.4.19-rt11.patch.xz

Extract the archive and apply the patch

    xz -cd linux-5.4.19.tar.xz | tar xvf -
    cd linux-5.4.19
    xzcat ../patch-5.4.19-rt11.patch.xz | patch -p1

## Configuration

copy over your old config and use that to configure your new kernel

    kautilya@johnny-5:~/factory/linux-5.4.19/ > cp /boot/config-5.3.0-40-generic .config
    kautilya@johnny-5:~/factory/linux-5.4.19/ > make oldconfig

when asked for Preemption Model, select the option "Fully Preemptible Kernel"
and accept the default value for the rest.

Alternatively, you could use the graphical interface to configure it using
menuconfig.

menuconfig requires flex and bison dependencies

    sudo apt install flex bison

open config editor using

    make menuconfig

search for `PREEMPT_RT` and set it to "Fully Preemptible Kernel (RT)".

## Build and Install

Build the kernel as a debian package using make command

    $ make -j8 deb-pkg
    $ sudo dpkg -i ../linux-headers-5.4.19-rt11_5.4.19-rt11-1_amd64.deb ../linux-image-5.4.19-rt11_5.4.19-rt11-1_amd64.deb ../linux-libc-dev_5.4.19-rt11-1_amd64.deb

## Verification

Reboot your system and check the kernel. It should show `PREEMPT_RT`

    kautilya@johnny-5:~/factory/linux-5.4.19/ > uname -a
    Linux johnny-5 5.4.19-rt11 #1 SMP PREEMPT_RT Fri Feb 21 12:54:56 PST 2020 x86_64 x86_64 x86_64 GNU/Linux

## Additional Configuration

### Security settings

Add your user to `realtime` group

    $ sudo groupadd realtime
    $ sudo usermod -aG realtime $USER

add the following to `/etc/security/limits.d/99-realtime.conf`

    $ sudo nano /etc/security/limits.d/99-realtime.conf
    
    @realtime soft rtprio 99
    @realtime soft priority 99
    @realtime soft memlock 102400
    @realtime hard rtprio 99
    @realtime hard priority 99
    @realtime hard memlock 102400

### CPU Scaling

disable cpu scaling by setting the cpu governer to `performance` using
cpufrequtils. 

    $ sudo apt install cpufrequtils

Check the available cpufreq governers using `cpufreq-info`, in my case they
were performance and powersave

    kautilya@johnny-5:~/ > cpufreq-info
    cpufrequtils 008: cpufreq-info (C) Dominik Brodowski 2004-2009
    Report errors and bugs to cpufreq@vger.kernel.org, please.
    analyzing CPU 0:
      driver: intel_pstate
      CPUs which run at the same hardware frequency: 0
      CPUs which need to have their frequency coordinated by software: 0
      maximum transition latency: 4294.55 ms.
      hardware limits: 800 MHz - 4.60 GHz
      available cpufreq governors: performance, powersave
      current policy: frequency should be within 800 MHz and 4.60 GHz.
                      The governor "performance" may decide which speed to use
                      within this range.
      current CPU frequency is 4.40 GHz.

set the cpu frequency to performance using the following

    $ sudo systemctl disable ondemand
    $ sudo systemctl enable cpufrequtils
    $ sudo sh -c 'echo "GOVERNOR=performance" > /etc/default/cpufrequtils'
    $ sudo systemctl daemon-reload && sudo systemctl restart cpufrequtils

### CPU Partitioning

I want to run a few programs that do not have a low latency requirements and
would like to isolate them from the real time programs. For this, I am going to
partition 2 of my 4 CPU cores to real time and other 2 to non realtime.

You can also set exclusively which CPU your program runs on by setting its
CPU affinity.


