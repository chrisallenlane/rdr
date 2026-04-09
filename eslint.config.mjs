export default [
  {
    files: ["static/js/**/*.js"],
    languageOptions: {
      ecmaVersion: 5,
      sourceType: "script",
      globals: {
        document: "readonly",
        history: "readonly",
        window: "readonly",
      },
    },
    rules: {
      "no-undef": "error",
      "no-unused-vars": "error",
      "no-redeclare": "error",
      eqeqeq: "error",
      "no-implicit-globals": "error",
    },
  },
];
