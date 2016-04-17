'use strict';

let fs = require('fs');
fs.readdirSync('./gulp').forEach(f => {
  if (f.endsWith('.js')) require('./gulp/'+f);
});

process.on('unhandledRejection', err => console.error('unhandledRejection', err));
process.on('uncaughtException', err => console.error('uncaughtException', err));
