#!/bin/sh
# Fake `systemctl show <unit> -p … --timestamp=unix` for the services smoke test.
[ "$1" = show ] || { echo "fake-systemctl: unsupported $*" >&2; exit 1; }
case "$2" in
  kangu.service) printf 'Id=kangu.service\nDescription=manual deployment for now for recipes app\nActiveState=active\nSubState=running\nMainPID=4242\nMemoryCurrent=104857600\nActiveEnterTimestamp=@1793300000\nUser=radu\n' ;;
  *) printf 'Id=%s\nDescription=\nActiveState=inactive\nSubState=dead\nMainPID=0\nMemoryCurrent=[not set]\nActiveEnterTimestamp=\nUser=\n' "$2" ;;
esac
