# Provider
Provider streams the iOS devices for controlfloor. This bridges the phone to the browser.

# Basic Install Instructions
## Clone repos
1. `git clone https://github.com/nanoscopic/ios_remote_provider.git`
1. `git clone https://github.com/nanoscopic/controlfloor.git`
1. `git clone https://github.com/nanoscopic/ios_video_app.git`
1. `git clone https://github.com/nanomsg/nng.git`

## Build nng - https://github.com/nanomsg/nng
1. `cd nng`
1. `cmake`
1. `make`
1. `make install`

## Build ControlFloor

1. `cd controlfloor`
1. `make`
1. `./main run`

Open `https://yourip:8080` to see if controlfloor is running

## Build iOS Remote Provider and WDA
1. `cd ios_remote_provider`
1. Edit `config.json` to add your Apple developer details
1. `make`
1. `security unlock-keychain login.keychain` # to make sure developer details are there for xcode build
1. `make wda`

## Register Provider
1. `./main register`

## Build and setup iOS Video App
1. `cd ios_video_app`
1. Open the xcode project and install vidtest2 on the device
1. Use Settings on the device to add "Screen Recording" to your Control Center if you haven't already. - https://www.youtube.com/watch?v=aWF-0Xdt3co

## Start iOS Video App
1. Open the Control Center on your device ( how depends on your device type )
1. Select Screen Recording
1. Choose vidtest2
1. Click "Start Recording"

## Start Provider
1. `cd ios_remote_provider`
1. `./main run`


