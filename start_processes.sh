#!/bin/bash

# Start the first process
imgproxy &

# Start the second process
proxy &

# Wait for any process to exit
wait -n

# Exit with status of process that exited first
exit $?