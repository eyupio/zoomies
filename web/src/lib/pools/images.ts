/**
 * The runner images a pool can boot, as the editor offers them.
 *
 * A pool stores one string, `image`, and that has not changed: empty follows the
 * operating system, anything else is used as written. What this module adds is
 * the reading of that string as a choice an operator can recognise, and the
 * words that say what each choice is. Nothing here is stored.
 *
 * The Docker variant is deliberately not a choice. A pool that gives its jobs a
 * daemon is moved onto it by the server as it is saved, so offering it beside the
 * others would invite a pool with a client and no daemon, or the other way
 * round. The Standard explanation says so instead.
 */
import type { PoolPlatform } from '$lib/api/types';

export type ImageFlavour = 'standard' | 'full' | 'custom';

export interface FlavourCopy {
  value: ImageFlavour;
  label: string;
  /** One sentence: what this is. */
  summary: string;
  /** What is in it. */
  contains: string;
  /** What kind of pool should choose it. */
  chooseWhen: string;
  /** What choosing it costs or leaves to the operator. */
  keepInMind: string;
}

/**
 * The words for each choice. They are about kinds of thing, never versions: the
 * toolchain versions move every week with `deploy/toolcache.lock`, and a number
 * written here would be wrong by the next release. The documentation carries the
 * list.
 */
export const FLAVOURS: readonly FlavourCopy[] = [
  {
    value: 'standard',
    label: 'Standard',
    summary:
      'The plain Zoomies runner on the operating system chosen above. It is the smallest image and the quickest to start, and it is what a pool gets when nothing is chosen.',
    contains:
      'The GitHub Actions runner, git and git-lfs, the GitHub CLI, compilers and build tools with the usual development headers, Python 3 and a current Node.js as a floor, and everyday tools such as curl, jq and zip.',
    chooseWhen:
      'Your jobs install the language versions they need with setup-node, setup-python, setup-go and the like, or you want the smallest image to pull.',
    keepInMind:
      'Each setup-* step downloads its toolchain unless the pool keeps a tool cache, which the Speed section can turn on. If Docker in jobs is on, Zoomies runs the same image with a Docker client added, so there is nothing to choose here.',
  },
  {
    value: 'full',
    label: 'Full toolchains',
    summary:
      'The standard runner with a Docker client and the language toolchains the setup-* actions would otherwise download, already installed where they look.',
    contains:
      'Python, Node.js, Go, Java (Eclipse Temurin), the .NET SDK, Maven, Gradle and Rust. The versions are the newest of each line and move forward every week.',
    chooseWhen:
      'Jobs spend their first minute in setup-go, setup-python or setup-java, or a host is often new to the pool. A step that asks for a version the image carries is answered from disk.',
    keepInMind:
      'It is several gigabytes larger, so it takes more disk on each host that runs the pool. It is published for the Ubuntu variants only. A version it does not carry still downloads, as it would anywhere else.',
  },
  {
    value: 'custom',
    label: 'Custom image',
    summary: 'An image you build or mirror, used exactly as you write it.',
    contains:
      'Whatever you put in it. It has to run the GitHub Actions runner the way ours does, so the safest way to make one is to start from one of ours.',
    chooseWhen:
      'You need extra system packages, a private registry mirror, internal certificates built in, or a digest to pin.',
    keepInMind:
      'Zoomies does not look inside it. A pool that gives jobs a Docker daemon needs a Docker client in the image, and Zoomies does not add one to an image it did not publish.',
  },
];

/** The copy for one choice. */
export function flavourCopy(flavour: ImageFlavour): FlavourCopy {
  return FLAVOURS.find((entry) => entry.value === flavour) ?? FLAVOURS[0]!;
}

/**
 * The platform an image choice is about: the one picked, or the catalogue's
 * default when the pool says "any", because that is the operating system an
 * empty choice boots.
 */
export function platformForImage(
  platforms: readonly PoolPlatform[],
  chosen: PoolPlatform | undefined,
): PoolPlatform | undefined {
  return chosen ?? platforms.find((entry) => entry.default) ?? platforms[0];
}

/** The toolchain image for a platform, or '' where none is published for it. */
export function fullImageOf(platform: PoolPlatform | undefined): string {
  return platform?.full_image ?? '';
}

/**
 * Which choice a pool's `image` string is.
 *
 * Empty is Standard. The toolchain image for the platform is Full. Anything else
 * is Custom, including a toolchain image for some other operating system or a
 * tag pinned by hand: it is kept exactly as written rather than quietly
 * re-pointed. `customPicked` is the operator having asked for Custom with the
 * field still empty, which the string alone cannot say.
 */
export function flavourOf(
  image: string,
  platform: PoolPlatform | undefined,
  customPicked = false,
): ImageFlavour {
  if (customPicked) return 'custom';
  const ref = image.trim();
  if (ref === '') return 'standard';
  const full = fullImageOf(platform);
  return full !== '' && ref === full ? 'full' : 'custom';
}

/**
 * Whether a reference is one of the published toolchain images, whatever
 * operating system or tag it names. A summary line can say "toolchain image"
 * for it without deciding, as the dropdown does, whether it is exactly the one
 * the chosen platform would pick.
 */
export function isToolchainImage(image: string): boolean {
  return /^ghcr\.io\/eyupio\/zoomies-runner-full(:[^@]+)?(@.+)?$/.test(image.trim());
}
