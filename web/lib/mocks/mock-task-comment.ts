export const mockTaskComments = [
  {
    id: "c1",
    taskId: "task-1",
    message: "Initial implementation completed.",
    createdAt: new Date().toISOString(),
    author: {
      id: "u1",
      name: "Alice",
    },
  },
  {
    id: "c2",
    taskId: "task-1",
    message: "Need API review before merge.",
    createdAt: new Date().toISOString(),
    author: {
      id: "u2",
      name: "Bob",
    },
  },
  {
    id: "c3",
    taskId: "task-1",
    message: "Waiting for QA verification.",
    createdAt: new Date().toISOString(),
    author: {
      id: "u3",
      name: "Sarah",
    },
  },
];
