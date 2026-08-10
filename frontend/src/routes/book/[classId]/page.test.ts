import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { goto } from "$app/navigation";
import { ApiError } from "$lib/api/errors";
import type { Booking, ClassList, Session } from "$lib/api/types";
import { classMutator, classReader } from "$lib/session/cached_api";
import { auth } from "$lib/stores/auth";
import { booking } from "$lib/stores/booking";
import { classes } from "$lib/stores/classes";
import BookPage from "./+page.svelte";

const classId = "0192a000-0000-7000-8000-000000000021";
const bookingId = "0192a000-0000-7000-8000-000000000031";

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$app/state", () => ({
    page: { params: { classId: "0192a000-0000-7000-8000-000000000021" } },
}));

vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

const session: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [
        { id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 },
        { id: "0192a000-0000-7000-8000-000000000012", full_name: "Arun Tan", grade_level: 3 },
    ],
};

const oneClass: ClassList = {
    classes: [
        {
            id: classId,
            subject: "science",
            title: "Science Discovery",
            starts_at: "2026-08-14T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 1,
        },
    ],
};

const heldBooking: Booking = {
    id: bookingId,
    student_id: "0192a000-0000-7000-8000-000000000011",
    class_id: classId,
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: "2026-08-11T09:10:00Z",
};

/** Picks a child and submits, the way a parent would. */
async function bookFor(studentId: string): Promise<void> {
    const options = (await screen.findAllByTestId("child-option")) as HTMLInputElement[];
    const chosen = options.find((option) => option.dataset.studentId === studentId);

    chosen?.click();

    await waitFor(() => {
        expect(screen.getByTestId("book-submit")).not.toBeDisabled();
    });

    screen.getByTestId("book-submit").click();
}

describe("the booking screen", () => {
    beforeEach(() => {
        vi.mocked(goto).mockClear();
        vi.mocked(classReader.read).mockReset().mockResolvedValue({
            body: oneClass,
            result: "fresh",
            revalidation: null,
        } as never);
        vi.mocked(classMutator.send).mockReset();

        auth.signIn(session);
        booking.reset();
        classes.reset();
    });

    test("integration: a granted hold moves the parent to the payment screen", async () => {
        vi.mocked(classMutator.send).mockResolvedValue(heldBooking as never);

        render(BookPage);

        await bookFor("0192a000-0000-7000-8000-000000000011");

        await waitFor(() => {
            expect(goto).toHaveBeenCalledWith(`/pay/${bookingId}`);
        });
    });

    test("integration: the class the parent picked is named on the screen", async () => {
        render(BookPage);

        await waitFor(() => {
            expect(screen.getByRole("heading", { name: "Science Discovery" })).toBeInTheDocument();
        });
    });

    test("integration: the seat count on this screen says plainly that it is a hint", async () => {
        // The whole design rests on nobody deciding from a cached count, and a
        // screen that presents one as fact teaches the opposite.
        render(BookPage);

        await waitFor(() => {
            expect(screen.getByText(/that count is a hint/i)).toBeInTheDocument();
        });
    });

    test("edge: nothing is sent until a child is picked", async () => {
        render(BookPage);

        await waitFor(() => {
            expect(screen.getByTestId("book-submit")).toBeDisabled();
        });

        screen.getByTestId("book-submit").click();

        expect(classMutator.send).not.toHaveBeenCalled();
    });

    test("edge: a class that filled while the parent was choosing says what happened", async () => {
        vi.mocked(classMutator.send).mockRejectedValue(new ApiError("ClassFull", "class_full", 409));

        render(BookPage);

        await bookFor("0192a000-0000-7000-8000-000000000012");

        await waitFor(() => {
            expect(screen.getByTestId("book-failure")).toHaveTextContent(
                "this class filled while you were choosing",
            );
        });

        expect(goto).not.toHaveBeenCalled();
    });

    test("edge: a duplicate offers the booking the child already has", async () => {
        const existing = "0192a000-0000-7000-8000-000000000099";

        vi.mocked(classMutator.send).mockRejectedValue(
            new ApiError("AlreadyBooked", "already_booked", 409, 0, "", existing),
        );

        render(BookPage);

        await bookFor("0192a000-0000-7000-8000-000000000011");

        await waitFor(() => {
            expect(screen.getByTestId("book-existing-link")).toHaveAttribute("href", `/booking/${existing}`);
        });
    });

    test("edge: a failure with no booking behind it offers no link to nowhere", async () => {
        vi.mocked(classMutator.send).mockRejectedValue(new ApiError("Unavailable", "internal_error", 500));

        render(BookPage);

        await bookFor("0192a000-0000-7000-8000-000000000011");

        await waitFor(() => {
            expect(screen.getByTestId("book-failure")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("book-existing-link")).not.toBeInTheDocument();
    });

    test("edge: an account with no children says so rather than showing an empty form", async () => {
        auth.signIn({ ...session, children: [] });

        render(BookPage);

        await waitFor(() => {
            expect(screen.getByTestId("child-picker-empty")).toBeInTheDocument();
        });

        expect(screen.getByTestId("book-submit")).toBeDisabled();
    });
});
