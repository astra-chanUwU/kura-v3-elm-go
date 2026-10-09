import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { Elm } = require("../dist/elm-tests.js");

const app = Elm.TestRunner.init();
let reported = false;
let reportTimeout;

app.ports.report.subscribe((results) => {
  reported = true;
  clearTimeout(reportTimeout);
  let failures = 0;

  for (const result of results) {
    const status = result.passed ? "PASS" : "FAIL";
    console.log(`${status} ${result.name}`);

    if (!result.passed) {
      failures += 1;
    }
  }

  console.log(`${results.length - failures}/${results.length} Elm checks passed`);
  process.exitCode = failures === 0 ? 0 : 1;
});

reportTimeout = setTimeout(() => {
  if (!reported) {
    console.error("Elm test runner did not report results");
    process.exitCode = 1;
  }
}, 5000);

app.ports.run.send(null);
