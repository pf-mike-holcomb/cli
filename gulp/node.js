'use strict';

let gulp   = require('gulp');
let gutil  = require('gulp-util');
let config = require('../config');
let fs     = require('mz/fs');
let http   = require('./http');
let tar    = require('tar');
let zlib   = require('zlib');
let mkdirp = require('mkdirp');
let path   = require('path');

exports.download = () => {
  let base = `node-v${config.nodeVersion}-darwin-x64`;
  let file = `./tmp/workspace/node`;
  return fs.exists(file)
  .then(exists => {
    if (exists) return;
    gutil.log(`${file} not found, fetching`);
    return http.get(`https://nodejs.org/download/release/v${config.nodeVersion}/${base}.tar.gz`)
    .then(res => {
      return new Promise((ok, fail) => {
        res.pipe(zlib.createGunzip()).pipe(tar.Parse())
        .on('entry', entry => {
          if (entry.props.path === `${base}/bin/node`) {
            mkdirp.sync(path.dirname(file));
            entry.pipe(fs.createWriteStream(file).on('error', fail).on('end', ok));
          }
        })
        .on('error', fail);
      });
    })
    .then(() => fs.chmod(file, 0o755));
  });
};

gulp.task('build:workspace:node', () => exports.download());
