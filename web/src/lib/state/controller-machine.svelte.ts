/**
 * The machine the controller runs on, for the page that draws it.
 *
 * Asked for once when the page opens and then followed on the stream: the
 * controller sends `controller.machine` whenever its sample moves, so nothing here
 * polls. A stream that lost its place says so (`resync`), and the answer is asked
 * for again, which is the one thing a missed frame needs.
 */
import { getControllerMachine } from '../api/client';
import { events } from '../api/sse';
import type { ControllerMachine } from '../hosts/machine';

class ControllerMachineState {
  #view = $state<ControllerMachine | null>(null);

  /** `null` until the controller has said. */
  get view(): ControllerMachine | null {
    return this.#view;
  }

  /** Take what the controller said, whole: a frame is the route's own shape. */
  adopt(view: ControllerMachine): void {
    this.#view = view;
  }

  /** Ask for the machine. A failure leaves what is known as it was; the next frame says. */
  refresh(): Promise<void> {
    return getControllerMachine()
      .then((view) => this.adopt(view))
      .catch(() => {
        // Not knowing draws no card, and the stream repairs it.
      });
  }

  /** Follow the stream for as long as the caller lives. Returns what stops it. */
  follow(): () => void {
    void this.refresh();
    const stops = [
      events.subscribe('controller.machine', (view) => this.adopt(view)),
      events.subscribe('resync', () => void this.refresh()),
    ];
    return () => stops.forEach((stop) => stop());
  }
}

export const controllerMachine = new ControllerMachineState();
