#!/usr/bin/env python3
from pbxproj import XcodeProject
import json

jsonPath = "muxed.json"
with open(jsonPath) as f:
  conf = json.load(f)

project = XcodeProject.load('./repos/vidapp/vidstream/vidstream.xcodeproj/project.pbxproj')

mode = "Release"

def getflag(target,flag):
  for conf in project.objects.get_configurations_on_targets(target, mode):
    cdict = conf["buildSettings"]
    return cdict[flag]

def removeflag(target,flag):
  val = ""
  for conf in project.objects.get_configurations_on_targets(target, mode):
    cdict = conf["buildSettings"]
    val = cdict[flag]
  if val is None:
    return
  project.remove_flags(flag, val, target, "Debug")

l = "vidstream"
r = "vidstream_ext"

vidstream = conf["vidapp"]
idPrefix = vidstream["bundleIdPrefix"]
project.set_flags('DEVELOPMENT_TEAM', vidstream["devTeamOu"], r, mode)
project.set_flags('DEVELOPMENT_TEAM', vidstream["devTeamOu"], l, mode)
project.set_flags('CODE_SIGN_STYLE', vidstream["main"]["buildStyle"], l, mode)
project.set_flags('CODE_SIGN_STYLE', vidstream["extension"]["buildStyle"], r, mode)
project.set_flags('PRODUCT_BUNDLE_IDENTIFIER', idPrefix + ".vidstream_ext", l, mode)
project.set_flags('PRODUCT_BUNDLE_IDENTIFIER', idPrefix + ".vidstream_ext.extension", r, mode)

lProv = vidstream["main"]["provisioningProfile"]
rProv = vidstream["extension"]["provisioningProfile"]

if lProv == "":
  removeflag(l, 'PROVISIONING_PROFILE_SPECIFIER')
else:
  project.set_flags('PROVISIONING_PROFILE_SPECIFIER', lProv, l, mode)

if rProv == "":
  removeflag(r, 'PROVISIONING_PROFILE_SPECIFIER')
else:
  project.set_flags('PROVISIONING_PROFILE_SPECIFIER', rProv, r, mode)

print("vidstream:")
print("  Style    : " + ( getflag(l, "CODE_SIGN_STYLE") or "nil" ) )
print("  Dev Team : " + ( getflag(l, "DEVELOPMENT_TEAM") or "nil" ) )
print("  Bundle ID: " + getflag(l, "PRODUCT_BUNDLE_IDENTIFIER") )
print("  Prov Prof: " + ( getflag(l, "PROVISIONING_PROFILE_SPECIFIER") or "nil" ) )

print("vidstream_ext:")
print("  Style    : " + ( getflag(r, "CODE_SIGN_STYLE") or "nil" ) )
print("  Dev Team : " + ( getflag(r, "DEVELOPMENT_TEAM") or "nil" ) )
print("  Bundle ID: " + getflag(r, "PRODUCT_BUNDLE_IDENTIFIER") )
print("  Prov Prof: " + ( getflag(r, "PROVISIONING_PROFILE_SPECIFIER") or "nil" ) )

project.save()