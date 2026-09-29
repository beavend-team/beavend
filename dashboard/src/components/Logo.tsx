import React from 'react';
import logoDark from '../assets/logo_dark.svg';
import logoLight from '../assets/logo_light.svg';

interface LogoProps {
  theme?: 'dark' | 'light';
  className?: string;
  alt?: string;
}

export const Logo: React.FC<LogoProps> = ({
  theme = 'dark',
  className = 'h-8 w-auto',
  alt = 'Beavend Logo',
}) => {
  return (
    <img
      src={theme === 'dark' ? logoDark : logoLight}
      alt={alt}
      className={className}
    />
  );
};
