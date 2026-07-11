import { expect } from "vitest";
import type { AxeMatchers } from "vitest-axe/matchers";
import * as axeMatchers from "vitest-axe/matchers";
import "@testing-library/jest-dom/vitest";

// vitest-axe ships its type augmentation as a Jest-style `declare global {
// namespace Vi }`, which Vitest 4's `Assertion` interface (declared under
// `declare module "vitest"`) does not pick up — and its "extend-expect"
// entry point has no runtime `expect.extend` call either. We wire both the
// runtime matcher and the matching Vitest-4-style module augmentation here.
expect.extend(axeMatchers);

declare module "vitest" {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- must match vitest's own Assertion<T = any> signature
  interface Assertion<T = any> extends AxeMatchers {}
  interface AsymmetricMatchersContaining extends AxeMatchers {}
}
