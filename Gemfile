source "https://rubygems.org"

# Plain Jekyll rather than the github-pages gem. The site is built by GitHub
# Actions (.github/workflows/deploy.yml), not GitHub's Pages builder, so it
# doesn't need the Pages plugin whitelist, and without the gem's forced safe
# mode the plugins in _plugins/ run. Versions match what github-pages 232 used.
gem "jekyll", "3.10.0"
gem "kramdown", "2.4.0" # 2.5 changes footnote markup
gem "kramdown-parser-gfm", "1.1.0"
gem "webrick", "~> 1.7"

group :jekyll_plugins do
  gem "jekyll-feed", "0.17.0"
  gem "jekyll-paginate", "1.1.0"
  gem "jekyll-redirect-from", "0.16.0"
  gem "jekyll-seo-tag", "2.8.0"
  gem "jekyll-sitemap", "1.4.0"
  gem "jemoji", "0.13.0"
  gem "jekyll-compose", "0.12.0"
end
