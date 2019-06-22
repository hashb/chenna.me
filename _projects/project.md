---
layout: project
title:  "Projects"
---


### Extrinsic Calibration of Stereo Camera and Velodyne LiDAR *June 2018*
-  Developed a ROS package to automate calibration between Velodyne VLP- 16 and ZED stereo camera.
-  Reduced the mean point to point error by 72% compared to manual feature based calibration.

### Real- time Semantic Segmentation on Low- Power Android Devices *May 2018*
- Developed a fast background subtraction for portrait video based on modified SegNet model.
- Model achieved a mean IoU of 87.3% at 30 FPS on Google Pixel 2.

### Estimating Depth from a single image using FCN Network *March 2018*
- Implemented a modified FCN Net and trained it on NYU Depth Dataset and KITTI Dataset.
- Model achieved a mean RMSE error of 0.294 on NYU Depth and 0.312 on KITTI Dataset.

### Object Detection and Segmentation in Point Cloud data using PointNet *January 2018*
- Trained modified PointNet model on YCB object dataset and BigBird dataset.
- Model runs at 24 fps on a NVIDIA GeForce 1060 GPU with an accuracy of 88.3%.

### Grasp Collision detection using Convolutional Neural Networks *Ongoing*
- Developed a CNN model to detect collisions btw robot and environment using PointClouds and JointState.
- Model classifies collisions with an accuracy of 84.7% and is \~30% faster than FCL.

### Simultaneous Robot State Estimation and Object Tracking *December 2017*
- Implemented an Extended Kalman Filter algorithm to estimate the object pose from noisy JointState.
- Used a Gaussian Mixture Model to plan a trajectory for Baxter arm to the object for grasping.

### Video Action recognition using Deep Learning *October 2017*
- Implemented a Bi- Directional LSTM Model on VGG16 Net using Keras to classify actions in scenes.
- Achieved a Mean Average Precision of 15.7 mAP compared to the State of the Art of 21.4 mAP.

### Autonomous Grasp Inference and Execution using Baxter and KUKA lwr4 Robots *January 2017*
- Designed an end- to- end grasping pipeline to grasp objects on a table autonomously.
- Training data was collected in Gazebo simulation and tested in real world. [ISRR 2017]

**Others**: Motion Planning: TrajOpt, RRT and Variants, RealTime RRT*; Image Segmentation with GMM, Image De- noising using MRF;
