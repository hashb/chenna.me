FROM ubuntu:20.04

# Set this before `apt-get` so that it can be done non-interactively
ENV DEBIAN_FRONTEND noninteractive
ENV TZ America/New_York
ENV LC_ALL C.UTF-8
ENV LANG C.UTF-8

COPY Gemfile /srv/jekyll/

WORKDIR /srv/jekyll

RUN apt update && apt install -y \
    # development packages
    ruby-dev \
    gcc \
    make \
    curl \
    build-essential \
    git-lfs \
    libc-dev \
    libffi-dev \
    zlib1g-dev \
    libxml2-dev \
    libgcrypt-dev \
    gnupg \
    libxslt-dev \
    python3 \
    # permissions to install packages
    sudo

RUN gem install bundler

# Set up user so that we can share ssh credentials.
RUN useradd -ms /bin/bash jekyll
ENV USERNAME=jekyll
RUN echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME\
    && chmod 0440 /etc/sudoers.d/$USERNAME

RUN bundle install && bundle update

# Set up git completion.
RUN echo "source /usr/share/bash-completion/completions/git" >> /home/$USERNAME/.bashrc
RUN echo "alias js='bundle exec jekyll serve'" >> /home/$USERNAME/.bashrc

EXPOSE 4000
