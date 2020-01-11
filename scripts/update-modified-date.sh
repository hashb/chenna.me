#!/bin/sh

# https://blog.nerde.pw/2016/08/09/jekyll-last-modified-date.html
echo ""
echo "************************************"
echo "*   updating last_modified_at...   *"
echo "************************************"
git diff --cached --name-status | while read a b; do
  echo "* Processing $b..."
  sed -i "/---.*/,/---.*/s/^last_modified_at:.*$/last_modified_at: $(date "+%Y-%m-%d %T %z")/" "$b"
  git add $b
done
echo ""
