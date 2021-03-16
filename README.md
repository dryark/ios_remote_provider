# Provider
Provider streams the iOS devices for controlfloor. This bridges the phone to the browser.

# Basic Install Instructions
* Clone repos
git clone ios_remote_provider
git clone controlfloor
git clone ios_video_app

* Build nng - https://github.com/nanomsg/nng
cmake, make, make install

* Build repos

cd controlfloor
make
./main run

Run https://yourip:8080 to see if controlfloor is running

cd ios_remote_provider
Edit config.json to add your Apple developer details
make
security unlock-keychain login.keychain #to make sure developer details are there for xcode build
make wda
./main register
./main run

cd ios_video_app
Open the xcode project and install vidtest2 on the device
Use Settings on the device to add "Screen Recording" to your Control Center if you haven't already. - https://www.youtube.com/watch?v=aWF-0Xdt3co
Open the Control Center on your device ( how depends on your device type )
Select Screen Recording
Choose vidtest2
Click "Start Recording"
Start provider.


