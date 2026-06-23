import { useState } from "react";
import reactLogo from "./assets/react.svg";
import viteLogo from "./assets/vite.svg";
import heroImg from "./assets/hero.png";

function App() {
  const [count, setCount] = useState(0);

  return (
    <>
      <section
        id="center"
        className="flex flex-grow flex-col place-content-center place-items-center gap-[18px] px-5 pt-8 pb-6 lg:gap-[25px] lg:p-0"
      >
        <div className="relative">
          <img
            src={heroImg}
            className="relative z-0 mx-auto inset-x-0 w-[170px]"
            width="170"
            height="179"
            alt=""
          />
          <img
            src={reactLogo}
            className="absolute inset-x-0 top-[34px] z-1 mx-auto h-[28px] [transform:perspective(2000px)_rotateZ(300deg)_rotateX(44deg)_rotateY(39deg)_scale(1.4)]"
            alt="React logo"
          />
          <img
            src={viteLogo}
            className="absolute inset-x-0 top-[107px] z-0 mx-auto h-[26px] w-auto [transform:perspective(2000px)_rotateZ(300deg)_rotateX(40deg)_rotateY(39deg)_scale(0.8)]"
            alt="Vite logo"
          />
        </div>
        <div>
          <h1 className="my-5 font-sans text-[36px] font-medium tracking-[-1.68px] text-text-h lg:my-8 lg:text-[56px]">
            Get started
          </h1>
          <p className="m-0">
            Edit{" "}
            <code className="inline-flex rounded bg-code-bg px-2 py-1 font-mono text-[15px] leading-[1.35] text-text-h">
              src/App.tsx
            </code>{" "}
            and save to test{" "}
            <code className="inline-flex rounded bg-code-bg px-2 py-1 font-mono text-[15px] leading-[1.35] text-text-h">
              HMR
            </code>
          </p>
        </div>
        <button
          type="button"
          className="mb-6 inline-flex rounded-[5px] border-2 border-transparent bg-accent-bg px-2.5 py-[5px] font-mono text-[16px] text-accent transition-colors duration-300 hover:border-accent-border focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
          onClick={() => setCount((count) => count + 1)}
        >
          Count is {count}
        </button>
      </section>

      <div className="relative w-full before:absolute before:top-[-4.5px] before:left-0 before:border-[5px] before:border-transparent before:border-l-border before:content-[''] after:absolute after:top-[-4.5px] after:right-0 after:border-[5px] after:border-transparent after:border-r-border after:content-['']"></div>

      <section
        id="next-steps"
        className="flex flex-col border-t border-border text-center lg:flex-row lg:text-left"
      >
        <div className="flex-1 border-b border-border px-5 py-6 lg:border-b-0 lg:border-r lg:p-8">
          <svg
            className="mb-4 h-[22px] w-[22px]"
            role="presentation"
            aria-hidden="true"
          >
            <use href="/icons.svg#documentation-icon"></use>
          </svg>
          <h2 className="mb-2 font-sans text-[20px] font-medium leading-[1.18] tracking-[-0.24px] text-text-h lg:text-[24px]">
            Documentation
          </h2>
          <p className="m-0">Your questions, answered</p>
          <ul className="mt-5 flex flex-wrap list-none justify-center gap-2 p-0 lg:mt-8 lg:flex-nowrap lg:justify-start">
            <li className="flex-1 basis-[calc(50%-8px)] lg:flex-none lg:basis-auto">
              <a
                href="https://vite.dev/"
                target="_blank"
                rel="noopener noreferrer"
                className="box-border flex w-full items-center justify-center gap-2 rounded-md bg-social-bg px-3 py-1.5 text-[16px] text-text-h no-underline transition-shadow duration-300 hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),0_4px_6px_-2px_rgba(0,0,0,0.05)] lg:w-auto lg:justify-start dark:hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.4),0_4px_6px_-2px_rgba(0,0,0,0.25)]"
              >
                <img className="h-[18px]" src={viteLogo} alt="" />
                Explore Vite
              </a>
            </li>
            <li className="flex-1 basis-[calc(50%-8px)] lg:flex-none lg:basis-auto">
              <a
                href="https://react.dev/"
                target="_blank"
                rel="noopener noreferrer"
                className="box-border flex w-full items-center justify-center gap-2 rounded-md bg-social-bg px-3 py-1.5 text-[16px] text-text-h no-underline transition-shadow duration-300 hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),0_4px_6px_-2px_rgba(0,0,0,0.05)] lg:w-auto lg:justify-start dark:hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.4),0_4px_6px_-2px_rgba(0,0,0,0.25)]"
              >
                <img className="h-[18px] w-[18px]" src={reactLogo} alt="" />
                Learn React
              </a>
            </li>
          </ul>
        </div>
        <div id="social" className="flex-1 px-5 py-6 lg:p-8">
          <svg
            className="mb-4 h-[22px] w-[22px]"
            role="presentation"
            aria-hidden="true"
          >
            <use href="/icons.svg#social-icon"></use>
          </svg>
          <h2 className="mb-2 font-sans text-[20px] font-medium leading-[1.18] tracking-[-0.24px] text-text-h lg:text-[24px]">
            Connect with us
          </h2>
          <p className="m-0">Join the Vite community</p>
          <ul className="mt-5 flex flex-wrap list-none justify-center gap-2 p-0 lg:mt-8 lg:flex-nowrap lg:justify-start">
            <li className="flex-1 basis-[calc(50%-8px)] lg:flex-none lg:basis-auto">
              <a
                href="https://github.com/vitejs/vite"
                target="_blank"
                rel="noopener noreferrer"
                className="box-border flex w-full items-center justify-center gap-2 rounded-md bg-social-bg px-3 py-1.5 text-[16px] text-text-h no-underline transition-shadow duration-300 hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),0_4px_6px_-2px_rgba(0,0,0,0.05)] lg:w-auto lg:justify-start dark:hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.4),0_4px_6px_-2px_rgba(0,0,0,0.25)]"
              >
                <svg
                  className="h-[18px] w-[18px] dark:invert dark:brightness-200"
                  role="presentation"
                  aria-hidden="true"
                >
                  <use href="/icons.svg#github-icon"></use>
                </svg>
                GitHub
              </a>
            </li>
            <li className="flex-1 basis-[calc(50%-8px)] lg:flex-none lg:basis-auto">
              <a
                href="https://chat.vite.dev/"
                target="_blank"
                rel="noopener noreferrer"
                className="box-border flex w-full items-center justify-center gap-2 rounded-md bg-social-bg px-3 py-1.5 text-[16px] text-text-h no-underline transition-shadow duration-300 hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),0_4px_6px_-2px_rgba(0,0,0,0.05)] lg:w-auto lg:justify-start dark:hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.4),0_4px_6px_-2px_rgba(0,0,0,0.25)]"
              >
                <svg
                  className="h-[18px] w-[18px] dark:invert dark:brightness-200"
                  role="presentation"
                  aria-hidden="true"
                >
                  <use href="/icons.svg#discord-icon"></use>
                </svg>
                Discord
              </a>
            </li>
            <li className="flex-1 basis-[calc(50%-8px)] lg:flex-none lg:basis-auto">
              <a
                href="https://x.com/vite_js"
                target="_blank"
                rel="noopener noreferrer"
                className="box-border flex w-full items-center justify-center gap-2 rounded-md bg-social-bg px-3 py-1.5 text-[16px] text-text-h no-underline transition-shadow duration-300 hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),0_4px_6px_-2px_rgba(0,0,0,0.05)] lg:w-auto lg:justify-start dark:hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.4),0_4px_6px_-2px_rgba(0,0,0,0.25)]"
              >
                <svg
                  className="h-[18px] w-[18px] dark:invert dark:brightness-200"
                  role="presentation"
                  aria-hidden="true"
                >
                  <use href="/icons.svg#x-icon"></use>
                </svg>
                X.com
              </a>
            </li>
            <li className="flex-1 basis-[calc(50%-8px)] lg:flex-none lg:basis-auto">
              <a
                href="https://bsky.app/profile/vite.dev"
                target="_blank"
                rel="noopener noreferrer"
                className="box-border flex w-full items-center justify-center gap-2 rounded-md bg-social-bg px-3 py-1.5 text-[16px] text-text-h no-underline transition-shadow duration-300 hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),0_4px_6px_-2px_rgba(0,0,0,0.05)] lg:w-auto lg:justify-start dark:hover:shadow-[0_10px_15px_-3px_rgba(0,0,0,0.4),0_4px_6px_-2px_rgba(0,0,0,0.25)]"
              >
                <svg
                  className="h-[18px] w-[18px] dark:invert dark:brightness-200"
                  role="presentation"
                  aria-hidden="true"
                >
                  <use href="/icons.svg#bluesky-icon"></use>
                </svg>
                Bluesky
              </a>
            </li>
          </ul>
        </div>
      </section>

      <div className="relative w-full before:absolute before:top-[-4.5px] before:left-0 before:border-[5px] before:border-transparent before:border-l-border before:content-[''] after:absolute after:top-[-4.5px] after:right-0 after:border-[5px] after:border-transparent after:border-r-border after:content-['']"></div>
      <section className="h-12 border-t border-border lg:h-[88px]"></section>
    </>
  );
}

export default App;
