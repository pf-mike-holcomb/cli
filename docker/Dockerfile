FROM ubuntu:14.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && \
  apt-get install software-properties-common python-software-properties && \
  add-apt-repository ppa:ubuntu-lxc/lxd-stable && \
  apt-get update && \
  apt-get upgrade -y && \
  apt-get -y install \
  autoconf \
  automake \
  build-essential \
  cowsay \
  curl \
  git \
  golang \
  jq \
  libcurl4-openssl-dev \
  libssl-dev \
  nsis \
  openssl \
  python-pip \
  python-dev \
  gnupg pbuilder ubuntu-dev-tools bzr-builddeb apt-file \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# osslsigncode
RUN curl -fsSLo osslsigncode-1.7.1.tar.gz https://cli-assets.heroku.com/osslsigncode-1.7.1.tar.gz && \
    tar -xvzf osslsigncode-1.7.1.tar.gz && cd osslsigncode-1.7.1 && \
    ./configure && make && make install && cd .. rm -rf osslsigncode-1.7.1

# python/aws
RUN pip install --upgrade pip && \
    pip install awscli

# setup repo
RUN mkdir -p $HOME/src/github.com/heroku && git clone https://github.com/heroku/cli $HOME/src/github.com/heroku/cli
WORKDIR $HOME/src/github.com/heroku/cli

ENV GOPATH=$HOME \
  PATH=$HOME/src/github.com/heroku/cli/tmp/dev/heroku/lib:$GOPATH/bin:/usr/local/go/bin:$PATH

RUN git checkout dev
RUN make deps build test -j4

COPY run run
ENTRYPOINT ["./run"]
