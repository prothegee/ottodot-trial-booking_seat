import { render, screen, waitFor } from "@testing-library/svelte";
import { describe, expect, test } from "vitest";

import PaymentForm from "./PaymentForm.svelte";

/** The props every case starts from. */
function baseProps() {
    return {
        amountCents: 4500,
        currency: "SGD",
        submitting: false,
        onSubmit: () => {},
    };
}

describe("the payment form", () => {
    test("unit: the amount is rendered for a person, from the cents the api uses", () => {
        // The api sends cents because money in a floating point number is how a
        // cent goes missing. This is the last moment before it is read.
        render(PaymentForm, { props: baseProps() });

        expect(screen.getByTestId("payment-amount")).toHaveTextContent("45.00");
    });

    test("edge: an amount that is not a round number of currency units still reads correctly", () => {
        render(PaymentForm, { props: { ...baseProps(), amountCents: 4599 } });

        expect(screen.getByTestId("payment-amount")).toHaveTextContent("45.99");
    });

    test("integration: submitting reports it once", async () => {
        let submitted = 0;

        render(PaymentForm, { props: { ...baseProps(), onSubmit: () => (submitted += 1) } });

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(submitted).toBe(1);
        });
    });

    test("behaviour: the control is dead for the whole in-flight window", async () => {
        // The double submit guard. The idempotency key would make a second call
        // harmless anyway, which is the point: this is the cheap layer on top
        // of the correct one, not instead of it.
        let submitted = 0;

        render(PaymentForm, {
            props: { ...baseProps(), submitting: true, onSubmit: () => (submitted += 1) },
        });

        const control = screen.getByTestId("payment-submit");

        expect(control).toBeDisabled();

        control.click();

        expect(submitted).toBe(0);
    });

    test("behaviour: a closed booking cannot be paid for", () => {
        let submitted = 0;

        render(PaymentForm, {
            props: { ...baseProps(), closed: true, onSubmit: () => (submitted += 1) },
        });

        expect(screen.getByTestId("payment-submit")).toBeDisabled();
        expect(screen.getByTestId("payment-submit")).toHaveTextContent("Payment closed");

        screen.getByTestId("payment-submit").click();

        expect(submitted).toBe(0);
    });

    test("unit: the control says it is a retry when the parent is trying again", () => {
        render(PaymentForm, { props: { ...baseProps(), retryOf: "PaymentDeclined" } });

        expect(screen.getByTestId("payment-submit")).toHaveTextContent("Try the payment again");
    });

    test("unit: the form says plainly that no card details are asked for", () => {
        // A mock payment screen that looked like a real one would be worse than
        // one that says what it is.
        render(PaymentForm, { props: baseProps() });

        expect(screen.getByText(/mock payment/i)).toBeInTheDocument();
    });

    test("edge: no field on this form asks for anything sensitive", () => {
        // There is nothing to type, so there is nothing to leak. The one input
        // is the submit control.
        const { container } = render(PaymentForm, { props: baseProps() });

        const typeable = container.querySelectorAll("input, textarea, select");

        expect(typeable).toHaveLength(0);
    });
});
