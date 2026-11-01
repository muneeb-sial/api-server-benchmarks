// k6 load script driven by the runner through environment variables:
//   BASE_URL      e.g. http://127.0.0.1:3000
//   SCENARIO      text | json
//   EXECUTOR      constant-vus | constant-arrival-rate
//   VUS           virtual users (constant-vus) / pre-allocated VUs (arrival-rate)
//   RATE          requests per second (constant-arrival-rate only)
//   DURATION      e.g. 30s
//   SUMMARY_PATH  where handleSummary writes the end-of-test JSON
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

// One count per response that failed any check (timeouts included), so a
// single bad request is not counted once per check.
const badResponses = new Counter('bad_responses');

const BASE_URL = __ENV.BASE_URL || 'http://127.0.0.1:3000';
const SCENARIO = __ENV.SCENARIO || 'text';
const EXECUTOR = __ENV.EXECUTOR || 'constant-vus';
const VUS = parseInt(__ENV.VUS || '100', 10);
const RATE = parseInt(__ENV.RATE || '10000', 10);
const DURATION = __ENV.DURATION || '30s';

const EXPECTED = {
  text: { path: '/text', body: 'hello world', type: 'text/plain' },
  json: { path: '/json', body: '{"message":"hello world"}', type: 'application/json' },
}[SCENARIO];

if (!EXPECTED) {
  throw new Error(`unknown SCENARIO "${SCENARIO}"`);
}

const scenario =
  EXECUTOR === 'constant-arrival-rate'
    ? {
        executor: 'constant-arrival-rate',
        rate: RATE,
        timeUnit: '1s',
        duration: DURATION,
        preAllocatedVUs: VUS,
        maxVUs: VUS * 4,
      }
    : { executor: 'constant-vus', vus: VUS, duration: DURATION };

export const options = {
  scenarios: { bench: scenario },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(90)', 'p(95)', 'p(99)'],
  discardResponseBodies: false,
  noConnectionReuse: false,
  insecureSkipTLSVerify: true,
};

const url = BASE_URL + EXPECTED.path;

export default function () {
  const res = http.get(url);
  const ok = check(res, {
    'status 200': (r) => r.status === 200,
    'body ok': (r) => normalize(r.body) === EXPECTED.body,
    'content-type ok': (r) => (r.headers['Content-Type'] || '').startsWith(EXPECTED.type),
  });
  if (!ok) badResponses.add(1);
}

// Frameworks differ in whitespace for JSON; compare structurally for json.
function normalize(body) {
  if (SCENARIO !== 'json' || typeof body !== 'string') return body;
  try {
    return JSON.stringify(JSON.parse(body));
  } catch (_) {
    return body;
  }
}

export function handleSummary(data) {
  const out = {};
  if (__ENV.SUMMARY_PATH) out[__ENV.SUMMARY_PATH] = JSON.stringify(data, null, 2);
  return out;
}
