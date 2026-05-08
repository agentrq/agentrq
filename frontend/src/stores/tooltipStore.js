import { defineStore } from 'pinia'

export const useTooltipStore = defineStore('tooltip', {
  state: () => ({
    visible: false,
    text: '',
    style: { top: '0px', left: '0px' }
  }),
  actions: {
    show(event, text, position = 'right') {
      const rect = event.currentTarget.getBoundingClientRect();
      
      let top = 0;
      let left = 0;

      if (position === 'bottom') {
        top = rect.bottom + 8;
        left = rect.left + (rect.width / 2);
      } else if (position === 'top') {
        top = rect.top - 8;
        left = rect.left + (rect.width / 2);
      } else {
        // default 'right'
        top = rect.top + (rect.height / 2);
        left = rect.right + 12;
      }

      this.visible = true;
      this.text = text;
      this.style = {
        top: `${top}px`,
        left: `${left}px`,
        transform: position === 'bottom' || position === 'top' ? 'translateX(-50%)' : 'translateY(-50%)'
      };
    },
    hide() {
      this.visible = false;
    }
  }
})
