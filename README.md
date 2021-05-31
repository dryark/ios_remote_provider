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

## Automatically starting Video App
1. Figure out your device id  
    A. `./bin/iosif list`  
1. Figure out your device UI width/height  
    This is the output of the `/session/[sid]/window/size` WDA command.  
  
    You may consider using the https://github.com/nanoscopic/ios_controller script to run this command easily. The window_size() function within `test.pl` of that repo does so.  
  
    Alternatively you can use curl/wget to directly make calls against WDA to create a session and then make the call. 
1. Figure out how Control Center is reached on your device.  
    It will be by swiping up from the bottom center of the screen, or down from the top right of the screen.
1. Add device specific config block to `config.json`:  
    ```  
    {
        ...
        devices:[
            {
                udid:"[your device id]"
                uiWidth:[your device width]
                uiHeight:[your device height]
                // bottomUp or topDown
                controlCenterMethod:"bottomUp"
            }
        ]
    }
    ```
1. That's it. The video app will be started automatically when the provider is started.

## Using tidevice instead of go-ios

You may wish to use tidevice instead of go-ios to start WDA. Do the following to get it setup:  
  
1. Install tidevice. `pip3 install tidevice`

1. Add a WDA start method to your `config.json`:  
    ```
    {
        ...
        wda:{
            ...
            startMethod: "tidevice"
        }
    }
    ```

1. Run `make usetidevice` to auto-generate the `calculated.json` file containing the location of tidevice installed on your system.  
  
1. Start provider normally; tidevice will be used.
