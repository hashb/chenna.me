# Containers run as a different user than the repo owner, and git refuses to
# read a repo it considers unsafe; jekyll-last-modified-at needs git log
git config --global --add safe.directory "$PWD"
bundle install
