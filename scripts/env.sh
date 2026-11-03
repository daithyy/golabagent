#!/bin/bash
>.env

while IFS= read -r line; do
  if [[ $line == \#* ]] || [[ -z $line ]]; then
    echo "$line" >>.env
    continue
  fi

  key="${line%%=*}"
  value="${line#*=}"

  if [ -n "$value" ]; then
    echo "$key=$value" >>.env
  else
    env_value="${!key}"
    if [ -n "$env_value" ]; then
      echo "$key=$env_value" >>.env
    else
      echo "$key=TODO_$key" >>.env
    fi
  fi
done <.env.example
