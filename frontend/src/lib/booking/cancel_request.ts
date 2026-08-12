/**
 * Withdrawing a hold, from the browser.
 *
 * It is separate from the booking store because it is not part of an attempt.
 * Creating and paying share one idempotency key so a retry cannot charge twice,
 * and a cancel has nothing to charge and nothing to retry into: it either moved
 * the booking or the booking had already moved on.
 *
 * The call goes through `classMutator` rather than the api client, so a freed
 * seat cannot leave a count on screen that it just made untrue.
 */
import type { TransportRequest } from "$lib/api/transport";
import type { Booking } from "$lib/api/types";
import { classMutator } from "$lib/session/cached_api";
import { bookingPathFor } from "$lib/stores/booking";

/**
 * What this needs to send a call, and nothing more.
 *
 * It is pinned to a Booking rather than left generic, because this file asks
 * exactly one question and the api gives exactly one shape back.
 */
export interface CancelSender {
    send(request: TransportRequest): Promise<Booking>;
}

/**
 * Withdraws one booking.
 *
 * Note:
 * - no idempotency key is sent, because the api asks for one only where money
 *   is involved. A cancel repeated is refused as a booking that already moved
 *   on, which is the honest answer rather than a replayed success.
 *
 * Param:
 * bookingId - string (which booking to withdraw)
 * sender - CancelSender (the mutator, injected only by a test)
 *
 * Return:
 * - the booking, now cancelled
 * - throws ApiError when the api refused it
 */
export async function requestedCancel(
    bookingId: string,
    sender: CancelSender = classMutator,
): Promise<Booking> {
    return sender.send({ method: "DELETE", path: bookingPathFor(bookingId) });
}
