FROM node:22-alpine

WORKDIR /app
COPY .next ./.next
COPY package*.json ./
RUN npm install --production --legacy-peer-deps

EXPOSE 3000
CMD ["npm", "start"]