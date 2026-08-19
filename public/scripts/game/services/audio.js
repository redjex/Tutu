const jailSound = new Audio('/assets/audio/jail.mp3');
jailSound.preload = 'auto';

export function playJailSound() {
  jailSound.currentTime = 0;
  jailSound.play().catch((error) => console.warn('Jail sound unavailable', error));
}
