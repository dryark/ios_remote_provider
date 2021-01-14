package main

import (
)

func proc_device_trigger( devTracker *DeviceTracker ) {
    o := ProcOptions{
        procName: "device_trigger",
        binary: devTracker.Config.iosDeployPath,
        args: []string{
            "-d",
            "-n", "test",
            "-t", "0",
        },
    }
        
    proc_generic( devTracker, nil, &o )
}