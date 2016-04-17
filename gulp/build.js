'use strict';

let gulp  = require('gulp');
let spawn = require('./spawn');

gulp.task('build', ['build:workspace']);

gulp.task('build:workspace', ['build:workspace:npm', 'build:workspace:node', 'build:workspace:cli']);

gulp.task('build:workspace:cli', [], () => spawn('go', ['build', '-o', './tmp/workspace/heroku']));
