---
layout: post
title: 'Snippets: ROS'
date: 2020-05-27 03:24 +0000
last_modified_at: 2020-05-27 03:33:23 +0000
tags: [Productivity, Tools]
published: true
---

I had been away from ROS world for a while now, but recently had to use ROS for
work. I have not worked with mobile robots before so the terminology is new to
me. In this post, I want to document things I learned about ROS. Currently, it
is focused on mobile robots.

<!-- more -->

> In a nutshell,
> 
> `odom` to `base_link` is the position of the robot in the inertial odometric 
> frame, as reported by some odometric sensor (like wheel  encoders)
> 
> `map` to `odom` is a correction introduced by localization or SLAM packages, 
> to account for odometric errors.
>
> `map` to `base_link` is therefore the corrected pose of the robot in the 
> inertial world frame.
>
> These are dynamic transforms, and different components of the navigation 
> stack are responsible for publishing them.
> copied from [^1] also checkout [^2]


[^1]: <https://answers.ros.org/question/10658/transform-base_link-to-base_lasermapodom/?answer=15727#post-id-15727>
[^2]: <https://www.ros.org/reps/rep-0105.html>
