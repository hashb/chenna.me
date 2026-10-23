source "https://rubygems.org"

# Plain Jekyll rather than the github-pages gem. The site is built by GitHub
# Actions (.github/workflows/deploy.yml), not GitHub's Pages builder, so it
# doesn't need the Pages plugin whitelist, and without the gem's forced safe
# mode the plugins in _plugins/ run.
gem "jekyll", "~> 4.4"

group :jekyll_plugins do
  gem "jekyll-feed", "~> 0.18"
  gem "jekyll-paginate", "~> 1.1"
  gem "jekyll-redirect-from", "~> 0.17"
  gem "jekyll-seo-tag", "~> 2.9"
  gem "jekyll-sitemap", "~> 1.4"
  gem "jemoji", "~> 0.13"
  gem "jekyll-compose", "~> 0.12"
  gem "jekyll-last-modified-at", "~> 1.3"
end
