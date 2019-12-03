#!/bin/sh

echo $PWD

stardate="$(echo $(./scripts/stardate $(date '+%Y-%m-%d')) | tr \[\]. -)"
base_path="./_posts/captains-log/"
file_name="$base_path$(date '+%Y-%m-%d')-captain-s-log-stardate$stardate.md"

echo "---" | tee -a $file_name
echo "layout: post" | tee -a $file_name
echo "title: Captain's log, stardate $(./scripts/stardate $(date '+%Y-%m-%d'))" | tee -a $file_name
echo "date: $(date '+%Y-%m-%d %T %z')" | tee -a $file_name
echo "last_modified_at: $(date '+%Y-%m-%d %T %z')" | tee -a $file_name
echo "tags: [Captain's log]" | tee -a $file_name
echo "---" | tee -a $file_name
echo "" | tee -a $file_name
echo "This week in review:" | tee -a $file_name
echo "" | tee -a $file_name
echo "<!-- more -->" | tee -a $file_name

echo $file_name

ls -lah $base_path
