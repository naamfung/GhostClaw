#!/bin/sh
# GhostClaw 构建脚本
cd builder/
go build -o builder.app builder.go
mv builder.app ../
cd ..
./builder.app help
