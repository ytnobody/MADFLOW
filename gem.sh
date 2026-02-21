#!/bin/bash

set -e

# Check if gmn command exists
if ! command -v gmn &> /dev/null
then
    echo "Error: 'gmn' command not found. Please ensure it is installed and in your PATH."
    exit 1
fi

GMN_PROMPT=""

# Check if a prompt is provided as an argument
if [ -n "$1" ]; then
    GMN_PROMPT="$1"
    echo "Using prompt from argument: \"$GMN_PROMPT\""
else
    # If no argument, prompt the user interactively
    read -p "Enter prompt for gmn: " GMN_PROMPT
fi


# This script wraps the 'gmn' command, retrying it with '--yolo -p' until it succeeds.

RETRY_DELAY=2

while true; do
  echo "Attempting to run gmn --yolo -p \"$GMN_PROMPT\""
  gmn --yolo -p "$GMN_PROMPT"
  if [ $? -eq 0 ]; then
    echo "gmn command succeeded."
    break
  else
    echo "gmn command failed with exit code $?. Please check the output above for details."
    read -p "Retry? (y/n/d for delay): " user_choice
    case "$user_choice" in
      y|Y )
        echo "Retrying in ${RETRY_DELAY} seconds..."
        sleep ${RETRY_DELAY}
        ;;
      d|D )
        read -p "Enter new delay in seconds (current: ${RETRY_DELAY}): " new_delay
        if [[ "$new_delay" =~ ^[0-9]+$ && "$new_delay" -gt 0 ]]; then
          RETRY_DELAY=$new_delay
          echo "Retry delay set to ${RETRY_DELAY} seconds."
        else
          echo "Invalid delay. Keeping current delay of ${RETRY_DELAY} seconds."
        fi
        ;;
      * )
        echo "Exiting."
        exit 1
        ;;
    esac
  fi
done