# frozen_string_literal: true

## Liquid filter 'fingerprint': adds a content hash to a CSS/JS URL
## Usage <link href="{{ '/assets/css/style.css' | fingerprint }}" rel="stylesheet">
##   => /assets/css/style.css?v=6f6233949c
##
## The hash only changes when the file does, so browsers keep their cached
## copy until there's something new to fetch.
require "digest"

module AssetFingerprint
  @digests = {}

  class << self
    # Forget last build's hashes (jekyll serve rebuilds the same Site)
    def reset
      @digests = {}
    end

    def digest(site, url)
      @digests[url] ||= begin
        content = content_for(site, url)
        content && Digest::SHA256.hexdigest(content)[0, 10]
      end
    end

    private

    # Pages with front matter (style.scss, bundle.js) are hashed by their
    # rendered output, plain files by their bytes
    def content_for(site, url)
      if (page = site.pages.find { |p| p.url == url })
        # Posts render before pages, so the asset may not be rendered yet
        page.output ||= Jekyll::Renderer.new(site, page, site.site_payload).run
      elsif (file = site.static_files.find { |f| f.url == url })
        File.binread(file.path)
      end
    end
  end

  module Filter
    def fingerprint(url)
      site = @context.registers[:site]
      digest = AssetFingerprint.digest(site, url)
      return "#{url}?v=#{digest}" if digest

      Jekyll.logger.warn "Fingerprint:", "#{url} isn't part of the site, left as is"
      url
    end
  end
end

Jekyll::Hooks.register :site, :after_reset do |_site|
  AssetFingerprint.reset
end

Liquid::Template.register_filter(AssetFingerprint::Filter)
