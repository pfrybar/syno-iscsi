## syno-iscsi

[![Build status](https://img.shields.io/github/actions/workflow/status/pfrybar/syno-iscsi/go.yml)](https://github.com/pfrybar/syno-iscsi/actions?workflow=go)
[![Software License](https://img.shields.io/github/license/pfrybar/syno-iscsi)](/LICENSE)
[![Release](https://img.shields.io/github/v/release/pfrybar/syno-iscsi)](https://github.com/pfrybar/syno-iscsi/releases/latest)

CLI for interacting with Synology iSCSI storage

### Description

Simple CLI written in Go that allows creating, listing, and deleting Synology
iSCSI LUNs and targets. Binaries are provided on the releases page.

Uses a module from the [synology-csi](https://github.com/SynologyOpenSource/synology-csi)
project for calling the Synology API.

Requires Synology DSM 7.0 or newer.

### Demo

![demo](docs/demo.gif)
