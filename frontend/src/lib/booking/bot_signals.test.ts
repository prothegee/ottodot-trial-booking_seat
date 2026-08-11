import { describe, expect, test } from "vitest";

import { botSignals, mockCaptchaToken, unmeasuredSignals } from "./bot_signals";

describe("the bot signals", () => {
    test("unit: the elapsed time is the difference between two instants", () => {
        const signals = botSignals("", 1000, 5200, mockCaptchaToken);

        expect(signals.filled_in_ms).toBe(4200);
    });

    test("unit: the honeypot is passed through exactly as it stands", () => {
        // Nothing here refuses it. A client that refused its own submission
        // would teach a script exactly which value to change.
        const signals = botSignals("http://cheap-pills.example", 0, 4200, "");

        expect(signals.website).toBe("http://cheap-pills.example");
    });

    test("unit: the challenge token is passed through untouched", () => {
        // The client never reads it, never shortens it, and never decides
        // anything from it, because only the provider can say what it means.
        const signals = botSignals("", 0, 4200, "opaque-token-from-a-provider");

        expect(signals.captcha_token).toBe("opaque-token-from-a-provider");
    });

    test("edge: a clock that moved backwards reports nothing measured", () => {
        // Zero is what the api reads as "not measured", which is the honest
        // answer when the measurement cannot be trusted. A negative would be a
        // number the api would have to invent a meaning for.
        const signals = botSignals("", 5000, 1000, "");

        expect(signals.filled_in_ms).toBe(0);
    });

    test("edge: a submission at the same instant reports nothing measured", () => {
        const signals = botSignals("", 1000, 1000, "");

        expect(signals.filled_in_ms).toBe(0);
    });

    test("edge: a fractional measurement is rounded to a whole millisecond", () => {
        // performance.now returns fractions. The api reads an integer, and a
        // fraction on the wire would be a number the column cannot hold.
        const signals = botSignals("", 100.4, 4300.9, "");

        expect(Number.isInteger(signals.filled_in_ms)).toBe(true);
        expect(signals.filled_in_ms).toBe(4201);
    });

    test("unit: the unmeasured set is what a caller with no form sends", () => {
        expect(unmeasuredSignals()).toEqual({ website: "", filled_in_ms: 0, captcha_token: "" });
    });

    test("unit: the mock token matches the one the backend's mock accepts", () => {
        // The two halves are testable end to end only because this string is
        // the same on both sides. A real provider replaces both at once.
        expect(mockCaptchaToken).toBe("mock-captcha-pass");
    });
});
