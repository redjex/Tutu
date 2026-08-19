import { diceFiles } from '../config.js';

async function readTgs(file) {
  const response = await fetch(file);
  if (!response.ok) throw new Error(`Не удалось загрузить ${file}`);
  const compressed = await response.arrayBuffer();
  const stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream('gzip'));
  return JSON.parse(await new Response(stream).text());
}

export class Dice {
  constructor(container) {
    this.container = container;
    this.animations = [];
    this.current = null;
  }

  async init() {
    try {
      this.animations = await Promise.all(diceFiles.map(readTgs));
      if (window.lottie && this.animations[0]) {
        this.current = window.lottie.loadAnimation({
          container:this.container,
          renderer:'svg',
          loop:false,
          autoplay:false,
          animationData:this.animations[0],
        });
        this.current.addEventListener('DOMLoaded', () => {
          this.current.goToAndStop(Math.max(0, this.animations[0].op - 1), true);
        });
      }
    } catch (error) {
      console.warn('TGS dice unavailable', error);
    }
  }

  async roll(value = null) {
    const result = Number.isInteger(value) && value >= 1 && value <= 6 ? value : Math.floor(Math.random() * 6) + 1;
    const animationData = this.animations[result - 1];
    if (!window.lottie || !animationData) return result;

    this.current?.destroy();
    this.container.replaceChildren();
    this.current = window.lottie.loadAnimation({
      container:this.container,
      renderer:'svg',
      loop:false,
      autoplay:true,
      animationData,
    });
    await new Promise((resolve) => {
      this.current.addEventListener('complete', resolve);
      this.current.addEventListener('data_failed', resolve);
    });
    return result;
  }
}
