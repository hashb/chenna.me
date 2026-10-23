# frozen_string_literal: true

## jekyll-last-modified-at gives every page a git-based last_modified_at,
## including the redirect pages jekyll-redirect-from generates. Those have no
## source file, so reading the date raises ENOENT and the build fails. Drop
## the date from pages that don't exist on disk (runs after the gem's hook).
Jekyll::Hooks.register :pages, :post_init, priority: :low do |page|
  next if File.exist?(Jekyll.sanitized_path(page.site.source, page.path))

  page.data.delete("last_modified_at")
end
