#!/bin/sh
# Fake `journalctl -u <unit> -n N -o json --no-pager`: two entries, one carrying a session cookie.
echo '{"MESSAGE":"started kangu","PRIORITY":"6","__REALTIME_TIMESTAMP":"1793300000000000"}'
echo '{"MESSAGE":"_session response: Set-Cookie AuthSession=c2VjcmV0dG9rZW4; Version=1","PRIORITY":"3","__REALTIME_TIMESTAMP":"1793300001000000"}'
