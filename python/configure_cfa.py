#!/usr/bin/env python3
from pbxproj import XcodeProject
import json

jsonPath = "muxed.json"
with open(jsonPath) as f:
  conf = json.load(f)

project = XcodeProject.load('./repos/CFAgent/CFAgent.xcodeproj/project.pbxproj')

def getflag(target,flag):
  for conf in project.objects.get_configurations_on_targets(target, "Debug"):
    cdict = conf["buildSettings"]
    return cdict[flag]

def removeflag(target,flag):
  val = ""
  for conf in project.objects.get_configurations_on_targets(target, "Debug"):
    cdict = conf["buildSettings"]
    val = cdict[flag]
  if val is None:
    return
  project.remove_flags(flag, val, target, "Debug")

l = "CFAgentLib"
r = "CFAgent"

cfa = conf["cfa"]
idPrefix = cfa["bundleIdPrefix"]
project.set_flags('DEVELOPMENT_TEAM', cfa["devTeamOu"], r, "Debug")
project.set_flags('DEVELOPMENT_TEAM', cfa["devTeamOu"], l, "Debug")
project.set_flags('CODE_SIGN_STYLE', cfa["lib"]["buildStyle"], l, "Debug")
project.set_flags('CODE_SIGN_STYLE', cfa["runner"]["buildStyle"], r, "Debug")
project.set_flags('PRODUCT_BUNDLE_IDENTIFIER', idPrefix + ".CFAgentLib", l, "Debug")
project.set_flags('PRODUCT_BUNDLE_IDENTIFIER', idPrefix + ".CFAgent", r, "Debug")

lProv = cfa["lib"]["provisioningProfile"]
rProv = cfa["runner"]["provisioningProfile"]

if lProv == "":
  removeflag(l, 'PROVISIONING_PROFILE_SPECIFIER')
else:
  project.set_flags('PROVISIONING_PROFILE_SPECIFIER', lProv, l, "Debug")

if rProv == "":
  removeflag(r, 'PROVISIONING_PROFILE_SPECIFIER')
else:
  project.set_flags('PROVISIONING_PROFILE_SPECIFIER', rProv, r, "Debug")

print("Lib:")
print("  Style    : " + ( getflag(l, "CODE_SIGN_STYLE") or "nil" ) )
print("  Dev Team : " + ( getflag(l, "DEVELOPMENT_TEAM") or "nil" ) )
print("  Bundle ID: " + getflag(l, "PRODUCT_BUNDLE_IDENTIFIER") )
print("  Prov Prof: " + ( getflag(l, "PROVISIONING_PROFILE_SPECIFIER") or "nil" ) )

print("Runner:")
print("  Style    : " + ( getflag(r, "CODE_SIGN_STYLE") or "nil" ) )
print("  Dev Team : " + ( getflag(r, "DEVELOPMENT_TEAM") or "nil" ) )
print("  Bundle ID: " + getflag(r, "PRODUCT_BUNDLE_IDENTIFIER") )
print("  Prov Prof: " + ( getflag(r, "PROVISIONING_PROFILE_SPECIFIER") or "nil" ) )

project.save()
